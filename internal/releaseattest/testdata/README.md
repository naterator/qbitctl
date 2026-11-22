# Release-attestation fixtures

These fixtures were recorded on 2026-11-22 for the qbitctl 1.2.1 release.

`release-1.2.1.bundle.json` is the first GitHub-initiated release attestation
returned by the public GitHub API for the annotated tag object
`b66576da27592c294fc8a4a99a75dcc9c4480274`:

```text
GET https://api.github.com/repos/naterator/qbitctl/git/ref/tags/1.2.1
GET https://api.github.com/repos/naterator/qbitctl/attestations/sha1:b66576da27592c294fc8a4a99a75dcc9c4480274?predicate_type=release&per_page=100
```

`github-trusted-root.json` is the Sigstore trusted root fetched through the
production `NewVerifier` TUF path at the same recording time. It is used only
by the offline golden test. The test does not contact GitHub, TUF, Fulcio, or
the timestamp authority.
