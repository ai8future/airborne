# Published-module verifier could fail after a successful proof

The verifier put a fresh Go module cache inside its temporary directory. Downloaded module source is read-only by design, so the EXIT trap's plain recursive removal failed after module listing, build, and test had all passed.

Cleanup now restores owner write permission before removal, preserves a real verification failure's exit status, and reports cleanup failure only when the verification itself succeeded. The immutable v1.10.11 graph fix is superseded by v1.10.12 with the corrected verifier.
