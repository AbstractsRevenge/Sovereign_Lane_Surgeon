# Vendored from github.com/google/blueprint/parser (Apache-2.0)

The canonical Soong Blueprint parser, vendored (5 pure-stdlib files: ast, modify, parser,
printer, sort) because the upstream module can't be `go get`'d cleanly (its pathtools/testdata
carries glob-char filenames that break `go mod`). Keeping the surgeon self-contained + portable.
Used by requalify.go for AST-safe `.bp` label rewriting (HARD RULE 3). Upstream Apache-2.0
headers preserved in each file.
