package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

// builderVersion is stamped into api.json so an app can say which build of the
// generator wrote it. An app vendors a copy of src/builder and is a fork from
// that moment; `gova inspect` compares this against the running binary, which
// catches the common case of syncing src/builder without rebuilding the image.
// Bump it whenever anything under src/builder changes.
const builderVersion = "2026-09-03.1"

const usage = `gova — the GOVA application builder.

Usage:
  gova <command> [flags]

Commands:
  inspect     Show the api.json manifest, the files on disk, and any divergence.
              Run this first.
  sql         Execute SQL against the app database. Run before creating a model.
  model       Generate a model for an existing table. Registers it in api.json;
              creates no route.
  page        Generate an HTML shell + JS module at a human-facing URL.
  handler     Generate one custom JSON endpoint under /api/v1/ and register it.
  resource    Generate a full CRUD resource: model, five handlers, and a page
              with a create form and delete buttons.
  version     Print the builder version.

Field syntax (model, resource):
  Comma-separated name:type pairs — "title:string,quantity:int,due_at:datetime"
  Types: string, int, float, boolean, timestamp.
  A DATETIME column must be declared timestamp, never string.
  name:ref:<model> is a foreign key; name:email|date|datetime|time|json is a
  string with a format hint.

Run "gova <command> -h" for a command's flags.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	out, err := run(os.Args[1], os.Args[2:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "gova: "+err.Error())
		os.Exit(1)
	}
	if out != "" {
		fmt.Println(out)
	}
}

// run dispatches one subcommand. Split out from main so tests can drive the
// whole CLI surface without a subprocess.
func run(command string, args []string) (string, error) {
	switch command {
	case "inspect":
		return inspect()

	case "sql":
		fs := flag.NewFlagSet("sql", flag.ContinueOnError)
		query := fs.String("query", "", "SQL to execute (required)")
		if err := fs.Parse(args); err != nil {
			return "", err
		}
		return executeSQL(*query)

	case "model":
		fs := flag.NewFlagSet("model", flag.ContinueOnError)
		name := fs.String("name", "", "model name, snake_case singular (required)")
		fields := fs.String("fields", "", "comma-separated name:type list (required)")
		if err := fs.Parse(args); err != nil {
			return "", err
		}
		return createModel(*name, splitFields(*fields))

	case "page":
		fs := flag.NewFlagSet("page", flag.ContinueOnError)
		file := fs.String("file", "", "filename without extension (required)")
		title := fs.String("title", "", "page title (required)")
		path := fs.String("path", "", "human-facing URL, e.g. /dashboard (required)")
		auth := fs.Bool("auth", false, "redirect signed-out visitors to /login")
		if err := fs.Parse(args); err != nil {
			return "", err
		}
		return createPage(*file, *title, *path, *auth)

	case "handler":
		fs := flag.NewFlagSet("handler", flag.ContinueOnError)
		name := fs.String("name", "", "handler name, snake_case (required)")
		method := fs.String("method", "", "GET, POST, PUT or DELETE (required)")
		path := fs.String("path", "", "full route path under /api/v1/ (required)")
		auth := fs.Bool("auth", false, "wrap the route in middleware.RequireAuth")
		summary := fs.String("summary", "", "one line describing what this endpoint does")
		reqSchema := fs.String("request-schema", "", `JSON body schema: {"shape":"object|list|empty","model":"<name>"?,"fields":[...]?}`)
		respSchema := fs.String("response-schema", "", "JSON body schema for the response data")
		if err := fs.Parse(args); err != nil {
			return "", err
		}
		return createHandler(*name, *method, *path, *auth, *summary, *reqSchema, *respSchema)

	case "resource":
		fs := flag.NewFlagSet("resource", flag.ContinueOnError)
		name := fs.String("name", "", "resource name, snake_case singular (required)")
		fields := fs.String("fields", "", "comma-separated name:type list (required)")
		if err := fs.Parse(args); err != nil {
			return "", err
		}
		return scaffoldResource(*name, splitFields(*fields))

	case "version":
		return builderVersion, nil

	case "help", "-h", "--help":
		return strings.TrimRight(usage, "\n"), nil
	}
	return "", fmt.Errorf("unknown command %q — run \"gova help\" for the list", command)
}

// splitFields turns "a:string,b:int" into ["a:string", "b:int"]. Empty entries
// are dropped so a trailing comma is not an error.
func splitFields(raw string) []string {
	out := []string{}
	for _, part := range strings.Split(raw, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
