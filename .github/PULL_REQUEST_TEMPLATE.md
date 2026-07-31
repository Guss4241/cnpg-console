<!--
Thanks for contributing to cnpg-console! Please read CONTRIBUTING.md first.
Keep the PR focused on one logical change.
-->

## Summary

<!-- What does this PR do and why? -->

Closes #

## Type of change

- [ ] Bug fix (non-breaking change that fixes an issue)
- [ ] New feature (non-breaking change that adds functionality)
- [ ] Breaking change (fix or feature that changes existing behavior)
- [ ] Documentation / chore

## How was this tested?

<!-- Describe the tests you ran. For write-path changes (PR creation, in-place
     edit, delete + prune), say how you validated it — never against a
     production cluster or repo. -->

- [ ] `make test` passes
- [ ] `make web-build && make build` compiles with the SPA embedded
- [ ] `gofmt -l .` is clean and `go vet ./...` passes
- [ ] `helm lint deploy/helm/cnpg-console` passes (if the chart changed)

## Checklist

- [ ] My change follows the style of the code I touched (French comments).
- [ ] I preserved the design invariants (PR-gated writes, two tokens / no direct
      cluster access, config-driven, clean textual diffs).
- [ ] I updated docs (README / chart values / CONTRIBUTING) where relevant.
- [ ] I added or updated tests (clusterspec / manifest rendering).
- [ ] **No secrets, tokens, or real credentials are included in this PR.**
- [ ] I have read and agree to the [Code of Conduct](../CODE_OF_CONDUCT.md).
