Bugfix: Fix unlock via space API

We fixed a bug that caused Error 500 when user try to unlock file using fileid
The handleSpaceUnlock has been added

https://github.com/cs3org/reva/pull/4338
https://github.com/owncloud/ocis/issues/7708
