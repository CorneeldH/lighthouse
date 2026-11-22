// Command servedir serves one directory over HTTP, for looking at a static site locally.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
)

const defaultAddr = "localhost:8000"

func handler(dir string) http.Handler {
	return http.FileServer(http.Dir(dir))
}

func parseArgs(args []string) (dir, addr string, err error) {
	fs := flag.NewFlagSet("servedir", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&addr, "addr", defaultAddr, "address to listen on")
	if err := fs.Parse(args); err != nil {
		return "", "", err
	}
	if fs.NArg() != 1 {
		return "", "", errors.New("usage: servedir [-addr host:port] <dir>")
	}
	return fs.Arg(0), addr, nil
}

func run(args []string, stdout, stderr io.Writer, serve func(addr string, h http.Handler) error) int {
	dir, addr, err := parseArgs(args)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		_, _ = fmt.Fprintln(stderr, "usage: servedir [-addr host:port] <dir>")
		return 2
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		_, _ = fmt.Fprintf(stderr, "servedir: %s is not a directory\n", dir)
		return 2
	}
	_, _ = fmt.Fprintf(stdout, "serving %s at http://%s/\n", dir, addr)
	if err := serve(addr, handler(dir)); err != nil {
		_, _ = fmt.Fprintln(stderr, "servedir:", err)
		return 1
	}
	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, http.ListenAndServe))
}
