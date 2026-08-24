## Summary

<!-- What does this PR do and why? Link the issue (e.g. Closes #N). -->

## Type of change

- [ ] Bug fix
- [ ] New feature
- [ ] Documentation / tooling
- [ ] Refactor (no behavior change)

## Verification

<!-- Commands run and their results. -->

- [ ] `gofmt -l .` prints nothing
- [ ] `go vet ./...` passes clean
- [ ] `golangci-lint run` passes
- [ ] `go test -race ./...` passes

## Checklist

- [ ] Tests cover all acceptance criteria; generated code reaches 90% unit-test coverage.
- [ ] No CGo — the binary still cross-compiles to `linux/amd64`, `linux/arm64`, `darwin/arm64`.
- [ ] `CHANGELOG.md` updated under `[Unreleased]`.
- [ ] `README.md` reflects what exists now (no aspirational/roadmap content).
- [ ] Hexagonal boundaries respected (components depend on ports, not concrete types).

## Notes for reviewers

<!-- Anything reviewers should know: trade-offs, follow-ups, unrelated issues noticed (but not fixed). -->
