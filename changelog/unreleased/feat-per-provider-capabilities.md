Enhancement: Declare storage capabilities per provider

Added a `providers` section to the `/ocs/v1.php/cloud/capabilities` response,
keyed by provider id, reporting the write actions each storage provider
supports. Drivers declare their capabilities through the optional
`storage.CapabilityProvider` interface and default to the full set when they do
not implement it. The global storage-capability keys (`files.undelete`,
`files.versioning`, `files.favorites` and `dav.trashbin`) are deprecated in
favor of the per-provider section.

https://github.com/owncloud/reva/pull/722
