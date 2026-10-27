"""Load ci/domains.py as a module: ci/ is no package."""

import importlib.util
from pathlib import Path
from types import ModuleType

PATH = Path(__file__).resolve().parents[1] / "ci" / "domains.py"


def load() -> ModuleType:
    spec = importlib.util.spec_from_file_location("lighthouse_ci_domains", PATH)
    assert spec is not None and spec.loader is not None, PATH
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module
