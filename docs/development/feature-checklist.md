# Feature change checklist

## Before implementation

- State the requested behavior and explicit platform scope.
- Identify API/domain/configuration callers, permissions, billing and privacy impact.
- Identify persistence changes and a non-destructive migration path.
- Confirm major changes to architecture, platform scope or security before coding.
- For a provider change, use provider-guide.md.

## During implementation

- Reuse the translation service and existing UI primitives.
- Handle applicable loading, empty, success, failure, cancellation and recovery.
- Keep native calls behind platform adapters and data access behind storage.
- Preserve user edits and avoid unused abstractions or compatibility branches.
- Add focused regression tests where they reduce risk.

## Before completion

- Run the shared checks and fix newly introduced failures.
- Keep dev/F5/build working and update scripts for new prerequisites/resources.
- Update documentation and tests for all directly affected contracts.
- Verify migrations without deleting user data.
- Report actual tests separately from unverified UI/OS/provider behavior.
- Do not claim cross-platform acceptance from compilation or a single OS test.
