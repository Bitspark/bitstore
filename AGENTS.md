# Working here as an agent

Read CHARTER.md and LAYOUT.md before changing this repository.

- Keep the public library independent of private packages and checkout paths.
- Follow the Deixis structural model and the explicit codec profile; keep raw
  content names separate from typed, version-qualified data roots.
- Write expected observations from the contract. Do not derive golden values
  from the implementation under test.
- Preserve published versions, contract identity, source provenance, and codec
  candidate status. Record migration boundaries and measured evidence.
- After the initial charter, work on a branch and land through a pull request,
  squashed onto main after the applicable checks pass.
- Validate Go formatting, vet, tests and races; TypeScript checks, builds and
  tests; and fresh external package consumers before release.
- Never commit credentials or local coordination state.
