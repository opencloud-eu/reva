Bugfix: Normalize OCM accepted-user ids on write

Accepted remote users are now stored with a bare OCM identifier in `opaque_id`
and a scheme-free provider in `idp`, matching the OCM specification. Legacy
entries (`uuid@https://host` opaque ids) remain readable via fallback matching.
`GetAcceptedUser` now returns `CODE_NOT_FOUND` instead of `CODE_INTERNAL` when
the remote user is unknown.

After OpenCloud bumps Reva, Graph invite `objectId` values from
`find-accepted-users` should use the single-`@` form `uuid@host:port` instead
of the previous double-`@` workaround.
