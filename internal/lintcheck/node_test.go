package lintcheck

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// nodeDelete removes a default file from nodeRepo.
const nodeDelete = "\x00"

const nodePkg = `{
  "name": "web",
  "scripts": {"lint": "eslint ."},
  "devDependencies": {"eslint": "10.13.0"}
}
`

const nodeConfig = "import js from '@eslint/js'\nexport default [{ extends: [js.configs.recommended] }]\n"

// nodeWorkflow is a push workflow with one job "lint" whose run steps work in web through
// defaults.run.working-directory; step i sits on line 9+i.
func nodeWorkflow(runs ...string) string {
	b := "on: [push]\njobs:\n  lint:\n    runs-on: ubuntu-latest\n    defaults:\n      run:\n        working-directory: web\n    steps:\n"
	for _, r := range runs {
		b += "      - run: " + r + "\n"
	}
	return b
}

// nodeRepo is a repo with a wired Node root in web; over replaces a file, or removes it with
// nodeDelete.
func nodeRepo(t *testing.T, over map[string]string) string {
	t.Helper()
	files := map[string]string{
		"web/package.json":      nodePkg,
		"web/eslint.config.js":  nodeConfig,
		"web/src/app.js":        "export const x = 1\n",
		wf + "ci.yml":           nodeWorkflow("npm ci", "npm run lint"),
		".lighthouse/README.md": "x\n",
	}
	for k, v := range over {
		if v == nodeDelete {
			delete(files, k)
			continue
		}
		files[k] = v
	}
	return writeRepo(t, files)
}

func checkNode(t *testing.T, root, dir string) ([]string, []string, error) {
	t.Helper()
	findings, wired, err := NodeChecker{}.Check(dir, Env{Root: root})
	var lines []string
	for _, f := range findings {
		lines = append(lines, FormatFinding(f))
	}
	return lines, wired, err
}

// wantNode asserts the exact findings for web, and that eslint is wired only without findings.
func wantNode(t *testing.T, over map[string]string, want ...string) {
	t.Helper()
	got, wired, err := checkNode(t, nodeRepo(t, over), "web")
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findings:\n got %q\nwant %q", got, want)
	}
	if len(want) == 0 && !reflect.DeepEqual(wired, []string{"eslint"}) {
		t.Fatalf("want eslint wired, got %v", wired)
	}
	if len(want) > 0 && len(wired) > 0 {
		t.Fatalf("wired with findings: %v", wired)
	}
}

func TestNodeWired(t *testing.T) {
	wantNode(t, nil)
}

func TestNodeNoCIStep(t *testing.T) {
	wantNode(t, map[string]string{wf + "ci.yml": nodeWorkflow("npm ci", "npm test")},
		".github/workflows: eslint — no CI step runs eslint in web")
}

func TestNodeLintInOtherDirDoesNotCount(t *testing.T) {
	// a valid eslint call for another package.json is no call for web
	other := "on: [push]\njobs:\n  lint:\n    runs-on: ubuntu-latest\n    steps:\n      - run: npm ci\n      - run: npx eslint .\n"
	wantNode(t, map[string]string{wf + "ci.yml": other, "package.json": nodePkg},
		".github/workflows: eslint — no CI step runs eslint in web")
}

func TestNodeEslintCaret(t *testing.T) {
	pkg := strings.Replace(nodePkg, `"10.13.0"`, `"^10.0.0"`, 1)
	wantNode(t, map[string]string{"web/package.json": pkg},
		"web/package.json: eslint — devDependencies eslint ^10.0.0 is not an exact version")
}

func TestNodeEslintNotInDevDependencies(t *testing.T) {
	pkg := `{"scripts": {"lint": "eslint ."}, "dependencies": {"eslint": "10.13.0"}}`
	wantNode(t, map[string]string{"web/package.json": pkg},
		"web/package.json: eslint — pin missing in devDependencies")
}

func TestExactVersion(t *testing.T) {
	for v, want := range map[string]bool{
		"10.13.0": true, "1.0.0-rc.1": true, "1.2.3+build.5": true,
		"^10.0.0": false, "~10.0.0": false, ">=10.0.0": false, "10.x": false, "10": false, "10.0": false,
		"latest": false, "*": false, "": false, "10.0.0 - 11.0.0": false, "10.0.0 || 11.0.0": false,
		"npm:eslint@10.0.0": false, "file:../eslint": false, " 10.13.0 ": false,
	} {
		if got := exactVersion(v); got != want {
			t.Errorf("exactVersion(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestNodeConfigOnlyIgnores(t *testing.T) {
	wantNode(t, map[string]string{"web/eslint.config.js": "export default [{ ignores: ['dist/**'] }]\n"},
		"web/eslint.config.js: eslint — config makes no choice (no rules, extends or preset)")
}

func TestNodeConfigSignOnlyInComment(t *testing.T) {
	cfg := "// rules: add later, extends js.configs.recommended\n/* eslint-config-expo */\nexport default [{ ignores: ['dist/**'] }]\n"
	wantNode(t, map[string]string{"web/eslint.config.js": cfg},
		"web/eslint.config.js: eslint — config makes no choice (no rules, extends or preset)")
}

func TestNodeConfigSignsOfChoice(t *testing.T) {
	for name, cfg := range map[string]string{
		"rules":              "export default [{ rules: { 'no-unused-vars': 'error' } }]\n",
		"rules key":          "export default [{ 'rules': { semi: 'error' } }]\n",
		"extends":            "export default defineConfig([{ extends: ['js/recommended'] }])\n",
		"configs.":           "import js from '@eslint/js'\nexport default [js.configs.recommended]\n",
		"preset":             "import x from 'eslint-config-standard'\nexport default [x]\n",
		"expo":               "const expoConfig = require('eslint-config-expo/flat')\nmodule.exports = [expoConfig]\n",
		"url first":          "// see https://eslint.org\nexport default [{ rules: { semi: 'error' } }]\n",
		"url in string":      "export default [{ settings: { docs: 'https://eslint.org' }, rules: { semi: 'error' } }]\n",
		"globs around rules": "export default [{ ignores: ['**/*.js'], rules: { semi: 'error' }, files: ['src/**/*.js'] }]\n",
		"glob double quotes": "export default [{ ignores: [\"**/*.js\"], rules: {}, files: [\"src/**/*.js\"] }]\n",
		"glob template":      "export default [{ ignores: [`**/*.js`], rules: {}, files: [`src/**/*.js`] }]\n",
		"slashes in string":  "export default [{ settings: { p: 'a//b' }, rules: { semi: 'error' } }]\n",
		"escaped quote":      "export default [{ settings: { p: 'it\\'s /* x' }, rules: { semi: 'error' } }]\n",
	} {
		t.Run(name, func(t *testing.T) {
			wantNode(t, map[string]string{"web/eslint.config.js": cfg})
		})
	}
}

func TestNodeConfigCommentsStillStripped(t *testing.T) {
	for name, cfg := range map[string]string{
		"block":            "/* rules: { semi: 'error' } */\nexport default [{ ignores: ['dist/**'] }]\n",
		"line":             "export default [{ ignores: ['dist/**'] }] // extends js.configs.recommended\n",
		"block after glob": "export default [{ ignores: ['**/*.js'] /* rules: {} */ }]\n",
		"line after slash": "export default [{ ignores: ['a//b'] }] // rules: {}\n",
		"quote in comment": "// it's eslint-config-expo\nexport default [{ ignores: ['dist/**'] }]\n",
	} {
		t.Run(name, func(t *testing.T) {
			wantNode(t, map[string]string{"web/eslint.config.js": cfg},
				"web/eslint.config.js: eslint — config makes no choice (no rules, extends or preset)")
		})
	}
}

func TestNodeConfigOtherNames(t *testing.T) {
	for _, name := range []string{"eslint.config.mjs", "eslint.config.cjs", "eslint.config.ts", ".eslintrc.json", ".eslintrc.yml", ".eslintrc.js", ".eslintrc"} {
		t.Run(name, func(t *testing.T) {
			cfg := "module.exports = { extends: ['eslint:recommended'] }\n"
			if strings.HasSuffix(name, ".yml") {
				cfg = "extends: eslint:recommended\n"
			}
			wantNode(t, map[string]string{"web/eslint.config.js": nodeDelete, "web/" + name: cfg})
		})
	}
	t.Run("yaml comment", func(t *testing.T) {
		wantNode(t, map[string]string{"web/eslint.config.js": nodeDelete, "web/.eslintrc.yml": "# extends: eslint:recommended\nignorePatterns: [dist]\n"},
			"web/.eslintrc.yml: eslint — config makes no choice (no rules, extends or preset)")
	})
}

func TestNodeConfigMissing(t *testing.T) {
	wantNode(t, map[string]string{"web/eslint.config.js": nodeDelete},
		"web/eslint.config.js: eslint — config missing")
}

func TestNodeConfigOutsideRootDoesNotCount(t *testing.T) {
	// only a config in the directory of the package.json counts
	wantNode(t, map[string]string{"web/eslint.config.js": nodeDelete, "eslint.config.js": nodeConfig},
		"web/eslint.config.js: eslint — config missing")
}

func TestNodeLockfileInstallRequired(t *testing.T) {
	wantNode(t, map[string]string{wf + "ci.yml": nodeWorkflow("npm install", "npm run lint")},
		".github/workflows/ci.yml:10: eslint — CI version not pinned")
	for _, inst := range []string{"pnpm install --frozen-lockfile", "yarn install --immutable", "yarn install --frozen-lockfile", "bun install --frozen-lockfile"} {
		t.Run(inst, func(t *testing.T) {
			wantNode(t, map[string]string{wf + "ci.yml": nodeWorkflow(inst, "npm run lint")})
		})
	}
}

func TestNodeLockfileInstallOtherJob(t *testing.T) {
	ci := "on: [push]\njobs:\n  install:\n    runs-on: ubuntu-latest\n    steps:\n      - run: npm ci\n        working-directory: web\n" +
		"  lint:\n    runs-on: ubuntu-latest\n    steps:\n      - run: npm run lint\n        working-directory: web\n"
	wantNode(t, map[string]string{wf + "ci.yml": ci},
		".github/workflows/ci.yml:11: eslint — CI version not pinned")
}

func TestNodeLockfileInstallAfterLint(t *testing.T) {
	wantNode(t, map[string]string{wf + "ci.yml": nodeWorkflow("npm run lint", "npm ci")},
		".github/workflows/ci.yml:9: eslint — CI version not pinned")
}

func TestNodeLockfileInstallOtherDir(t *testing.T) {
	ci := "on: [push]\njobs:\n  lint:\n    runs-on: ubuntu-latest\n    steps:\n      - run: npm ci\n      - run: npm run lint\n        working-directory: web\n"
	wantNode(t, map[string]string{wf + "ci.yml": ci},
		".github/workflows/ci.yml:7: eslint — CI version not pinned")
}

func TestNodeFlagNoConfigLookup(t *testing.T) {
	wantNode(t, map[string]string{wf + "ci.yml": nodeWorkflow("npm ci", "npx eslint --no-config-lookup .")},
		".github/workflows/ci.yml:10: eslint — CI flag --no-config-lookup drops the config")
}

func TestNodeConfigFlagNoChoice(t *testing.T) {
	wantNode(t, map[string]string{
		"web/ci.eslint.config.js": "export default [{ ignores: ['dist'] }]\n",
		wf + "ci.yml":             nodeWorkflow("npm ci", "npx eslint --config ci.eslint.config.js ."),
	}, ".github/workflows/ci.yml:10: eslint — CI config ci.eslint.config.js makes no choice")
	wantNode(t, map[string]string{
		wf + "ci.yml": nodeWorkflow("npm ci", "npx eslint --config missing.js ."),
	}, ".github/workflows/ci.yml:10: eslint — CI config missing.js makes no choice")
	// a config with a choice, given by flag, is fine
	wantNode(t, map[string]string{
		"web/ci.eslint.config.js": nodeConfig,
		wf + "ci.yml":             nodeWorkflow("npm ci", "npx eslint --no-config-lookup --config ci.eslint.config.js ."),
	})
}

func TestNodeExpo(t *testing.T) {
	pkg := `{
  "name": "app",
  "scripts": {"lint": "expo lint"},
  "dependencies": {"expo": "~54.0.0"},
  "devDependencies": {"eslint": "9.39.1", "eslint-config-expo": "~10.0.0"}
}
`
	cfg := "const { defineConfig } = require('eslint/config')\nconst expoConfig = require('eslint-config-expo/flat')\nmodule.exports = defineConfig([expoConfig])\n"
	over := map[string]string{"web/package.json": pkg, "web/eslint.config.js": cfg}
	wantNode(t, over)
	over[wf+"ci.yml"] = nodeWorkflow("npm ci", "npx expo export")
	wantNode(t, over, ".github/workflows: eslint — no CI step runs eslint in web")
}

func TestNodeNoScriptsNoDevDependencies(t *testing.T) {
	wantNode(t, map[string]string{"web/package.json": `{"name": "web"}`},
		"web/package.json: eslint — pin missing in devDependencies",
		".github/workflows: eslint — no CI step runs eslint in web")
}

func TestNodeInvalidPackageJSONIsUnchecked(t *testing.T) {
	for name, body := range map[string]string{
		"syntax":     `{"name": `,
		"wrong type": `{"devDependencies": ["eslint"]}`,
		"missing":    nodeDelete,
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := checkNode(t, nodeRepo(t, map[string]string{"web/package.json": body}), "web")
			var u UncheckedError
			if !errors.As(err, &u) {
				t.Fatalf("want UncheckedError, got %v", err)
			}
		})
	}
}

func TestNodeRootDot(t *testing.T) {
	root := writeRepo(t, map[string]string{
		"package.json":     nodePkg,
		"eslint.config.js": nodeConfig,
		wf + "ci.yml":      "on: [push]\njobs:\n  lint:\n    runs-on: ubuntu-latest\n    steps:\n      - run: npm ci\n      - run: npm run lint\n",
	})
	got, wired, err := checkNode(t, root, ".")
	if err != nil || len(got) != 0 || !reflect.DeepEqual(wired, []string{"eslint"}) {
		t.Fatalf("got %q %v %v", got, wired, err)
	}
	// the call's Dir and the checker's dir both normalise to ".": ./ as working-directory, ./ as dir
	root = writeRepo(t, map[string]string{
		"package.json":     nodePkg,
		"eslint.config.js": nodeConfig,
		wf + "ci.yml":      "on: [push]\njobs:\n  lint:\n    runs-on: ubuntu-latest\n    defaults:\n      run:\n        working-directory: ./\n    steps:\n      - run: npm ci\n      - run: npm run lint\n",
	})
	got, wired, err = checkNode(t, root, "./")
	if err != nil || len(got) != 0 || !reflect.DeepEqual(wired, []string{"eslint"}) {
		t.Fatalf("./: got %q %v %v", got, wired, err)
	}
}

// runNode runs the gate on node without Dirs, so importcheck.Discover picks the roots.
func runNode(t *testing.T, root string) (int, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(Options{Root: root, Langs: []Lang{"node"}, Checkers: map[Lang]Checker{"node": NodeChecker{}}},
		Env{Stdout: &out, Stderr: &errb})
	return code, out.String() + errb.String()
}

func TestNodeWorkspaceChildNoFinding(t *testing.T) {
	pkg := `{"name": "mono", "private": true, "workspaces": ["packages/*"], "scripts": {"lint": "eslint ."}, "devDependencies": {"eslint": "10.13.0"}}`
	root := writeRepo(t, map[string]string{
		"package.json":            pkg,
		"eslint.config.js":        nodeConfig,
		"packages/a/package.json": `{"name": "a"}`,
		"packages/a/src/index.js": "export const a = 1\n",
		wf + "ci.yml":             "on: [push]\njobs:\n  lint:\n    runs-on: ubuntu-latest\n    steps:\n      - run: npm ci\n      - run: npm run lint\n",
		"packages/b/package.json": `{"name": "b", "devDependencies": {"eslint": "^9.0.0"}}`,
	})
	code, out := runNode(t, root)
	if code != 0 || strings.Contains(out, "packages/") || !strings.Contains(out, "eslint: wired") {
		t.Fatalf("code %d, output:\n%s", code, out)
	}
}

func TestNodeTestdataFixtureIgnored(t *testing.T) {
	root := nodeRepo(t, map[string]string{"testdata/x/package.json": `{"name": "fixture"}`, "web/testdata/y/package.json": "{"})
	code, out := runNode(t, root)
	if code != 0 || strings.Contains(out, "testdata") || !strings.Contains(out, "eslint: wired") {
		t.Fatalf("code %d, output:\n%s", code, out)
	}
}

// Classification: cli-output. Fails when a run over two Node roots, one wired and one without a
// CI step, does not name the root in either line.
func TestNodeMultiRootOutput(t *testing.T) {
	root := nodeRepo(t, map[string]string{
		"app/package.json":     strings.Replace(nodePkg, `"web"`, `"app"`, 1),
		"app/eslint.config.js": nodeConfig,
	})
	code, out := runNode(t, root)
	want := ".github/workflows: eslint — no CI step runs eslint in app\neslint: wired (web)\n"
	if code != 1 || out != want {
		t.Fatalf("code %d, output %q, want %q", code, out, want)
	}
}
