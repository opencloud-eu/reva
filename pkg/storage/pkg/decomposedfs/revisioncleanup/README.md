# Revision cleanup

Every change to a file keeps the previous content as a revision (version).
Without a cleanup, revisions accumulate forever and use up storage.
The revision cleanup deletes old revisions according to a configurable strategy, whenever a file gets a new revision.

It works for the `decomposed`, `decomposeds3` and `posix` storage drivers.
With posix it also covers the revisions created from changes made directly on disk (`enable_fs_revisions`).
For how those drivers are structured, see [`../README.md`](../README.md); for implementation details, see the package documentation in `cleanup.go`.
