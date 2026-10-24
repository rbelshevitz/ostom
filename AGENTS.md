# Linux & Go Token Optimization
- Use Linux features to suppress stdout token waste.
- ALWAYS use `-trimpath` for `go build`, `go run`, and `go test`.
- NEVER use `go test -v`. Run tests as: `go test -trimpath ./... | head -n 30`
- If running test suites, always redirect or filter output to minimize token usage (e.g., use head or grep for failures).
- To check failures, run specific test: `go test -trimpath -run=TestName ./pkg/...`
- Suppress successful builds: `go build -trimpath -o app ./cmd/app > /dev/null`
- Use `golangci-lint run --max-issues-per-linter 3 --max-same-issues 3`
- Avoid running multiple full-suite tests in a single prompt. Run specific tests using the `-run` flag.
- Keep Go's build cache inside the workspace (for example, set `GOCACHE` to `.tmp-go-cache`) because the global user cache may be inaccessible in the managed environment.

# Documentation proofreading
- After adding or changing documentation, proofread the complete affected text,
  not only the edited lines.
- For translated README files and technical guides, check grammar, tone,
  terminology, links, command examples, and locale-specific UI names.
- Distinguish localized names shown in an application interface from fixed
  filesystem paths, file names, and CLI identifiers.
- When no native-language reviewer is available, verify terminology against
  official product documentation and describe the result as technical review,
  not native editorial review.
