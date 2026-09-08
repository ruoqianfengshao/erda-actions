## 2026-09-08 - Task: Remove customer-specific endpoint handling with master history rewrite

### What was done

- Removed customer-specific download URL rewriting from the release upload path.
- Added a contribution rule requiring customer-specific identifiers, endpoints, credentials, and deployment logic to remain outside the public repository.
- Prepared a master-only history rewrite to remove the same customer-specific strings from reachable history.

### Testing

- The pre-rewrite scan confirmed the reported customer-specific content was present in `master` history.
- Post-rewrite content, history, reference, and package-test verification completed locally; no remote force push was performed.
- The original reported commit and the first customer-specific commit are no longer reachable from the rewritten `master` ref.

### Notes

- `actions/release/1.0/internal/pkg/file.go`: removed the customer-specific download URL rewrite block.
- `docs/CONTRIBUTING.md`: documented public repository content restrictions.
- `progress.md`: recorded this master-only cleanup task.
- Rollback before remote push: discard this disposable clone. Rollback after push: restore the saved remote ref and coordinate a reversal with repository administrators; do not merge old history back into `master`.
