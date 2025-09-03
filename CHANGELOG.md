# Changelog

## [2.38.0](https://github.com/opencloud-eu/reva/releases/tag/v2.38.0) - 2025-09-03

### ❤️ Thanks to all contributors! ❤️

@individual-it, @rhafer

### 📚 Documentation

- delete outdated run instructions [[#334](https://github.com/opencloud-eu/reva/pull/334)]
- update obsolete branch name and delete version schema [[#333](https://github.com/opencloud-eu/reva/pull/333)]
- fix references to drone [[#332](https://github.com/opencloud-eu/reva/pull/332)]
- delete wrong references to the docs [[#331](https://github.com/opencloud-eu/reva/pull/331)]

### 📈 Enhancement

- feat: tracing instrumentation of for users and groups services [[#330](https://github.com/opencloud-eu/reva/pull/330)]

### 📦️ Dependencies

- Bump github.com/beevik/etree from 1.5.1 to 1.6.0 [[#328](https://github.com/opencloud-eu/reva/pull/328)]

## [2.37.0](https://github.com/opencloud-eu/reva/releases/tag/v2.37.0) - 2025-09-01

### ❤️ Thanks to all contributors! ❤️

@aduffeck, @dragonchaser, @individual-it, @rhafer, @tammi-23

### ✨ Features

- add UserSoftDelete events [[#317](https://github.com/opencloud-eu/reva/pull/317)]

### 🐛 Bug Fixes

- fix(posixfs): Ignore Events for Spaceroots [[#310](https://github.com/opencloud-eu/reva/pull/310)]
- Only send TrashbinPurged if there is no key [[#305](https://github.com/opencloud-eu/reva/pull/305)]

### 📦️ Dependencies

- Bump github.com/onsi/ginkgo/v2 from 2.25.1 to 2.25.2 [[#327](https://github.com/opencloud-eu/reva/pull/327)]
- Bump github.com/onsi/gomega from 1.38.0 to 1.38.2 [[#325](https://github.com/opencloud-eu/reva/pull/325)]
- Bump github.com/segmentio/kafka-go from 0.4.48 to 0.4.49 [[#323](https://github.com/opencloud-eu/reva/pull/323)]
- Bump github.com/hashicorp/go-plugin from 1.6.3 to 1.7.0 [[#321](https://github.com/opencloud-eu/reva/pull/321)]
- Bump github.com/prometheus/client_golang from 1.22.0 to 1.23.0 [[#320](https://github.com/opencloud-eu/reva/pull/320)]
- Bump google.golang.org/protobuf from 1.36.7 to 1.36.8 [[#319](https://github.com/opencloud-eu/reva/pull/319)]
- Bump github.com/coreos/go-oidc/v3 from 3.14.1 to 3.15.0 [[#318](https://github.com/opencloud-eu/reva/pull/318)]
- Bump github.com/nats-io/nats.go from 1.44.0 to 1.45.0 [[#316](https://github.com/opencloud-eu/reva/pull/316)]
- Bump google.golang.org/grpc from 1.74.0 to 1.75.0 [[#315](https://github.com/opencloud-eu/reva/pull/315)]
- Bump github.com/mattn/go-sqlite3 from 1.14.28 to 1.14.32 [[#314](https://github.com/opencloud-eu/reva/pull/314)]
- Bump github.com/emvi/iso-639-1 from 1.1.0 to 1.1.1 [[#313](https://github.com/opencloud-eu/reva/pull/313)]
- Bump github.com/golang-jwt/jwt/v5 from 5.2.3 to 5.3.0 [[#312](https://github.com/opencloud-eu/reva/pull/312)]
- Bump github.com/onsi/ginkgo/v2 from 2.23.4 to 2.24.0 [[#311](https://github.com/opencloud-eu/reva/pull/311)]
- Bump github.com/ceph/go-ceph from 0.34.0 to 0.35.0 [[#307](https://github.com/opencloud-eu/reva/pull/307)]
- Bump google.golang.org/protobuf from 1.36.6 to 1.36.7 [[#301](https://github.com/opencloud-eu/reva/pull/301)]
- Bump golang.org/x/crypto from 0.40.0 to 0.41.0 [[#303](https://github.com/opencloud-eu/reva/pull/303)]
- Bump github.com/nats-io/nats.go from 1.43.0 to 1.44.0 [[#302](https://github.com/opencloud-eu/reva/pull/302)]
- Bump go.etcd.io/etcd/client/v3 from 3.6.2 to 3.6.4 [[#300](https://github.com/opencloud-eu/reva/pull/300)]
- Bump github.com/onsi/gomega from 1.37.0 to 1.38.0 [[#291](https://github.com/opencloud-eu/reva/pull/291)]
- Bump github.com/minio/minio-go/v7 from 7.0.94 to 7.0.95 [[#290](https://github.com/opencloud-eu/reva/pull/290)]

## [2.36.0](https://github.com/opencloud-eu/reva/releases/tag/v2.36.0) - 2025-08-11

### ❤️ Thanks to all contributors! ❤️

@aduffeck, @dragonchaser

### ✨ Features

- add tenant check for shares [[#295](https://github.com/opencloud-eu/reva/pull/295)]

### 🐛 Bug Fixes

- Check storage for writability and xattrs support during startup [[#296](https://github.com/opencloud-eu/reva/pull/296)]
- Do not assimilate irregular files [[#294](https://github.com/opencloud-eu/reva/pull/294)]
- Only scan dirty directories when recursing [[#292](https://github.com/opencloud-eu/reva/pull/292)]

### 📈 Enhancement

- Filter users by tenant id [[#297](https://github.com/opencloud-eu/reva/pull/297)]

## [2.35.0](https://github.com/opencloud-eu/reva/releases/tag/v2.35.0) - 2025-07-21

### ❤️ Thanks to all contributors! ❤️

@aduffeck, @dragonchaser, @individual-it, @rhafer

### 🐛 Bug Fixes

- Do not leave .trashinfo files behind when restoring items [[#289](https://github.com/opencloud-eu/reva/pull/289)]
- log middleware: Implement Unwrap() method [[#285](https://github.com/opencloud-eu/reva/pull/285)]
- Fix read permissions crash [[#272](https://github.com/opencloud-eu/reva/pull/272)]

### 📈 Enhancement

- Add a raw package for directly consuming nats events [[#270](https://github.com/opencloud-eu/reva/pull/270)]
- Set 'oc:downloadURL' WebDAV property for "normal" files [[#271](https://github.com/opencloud-eu/reva/pull/271)]

### 📦️ Dependencies

- Bump google.golang.org/grpc from 1.73.0 to 1.74.0 [[#286](https://github.com/opencloud-eu/reva/pull/286)]
- Bump github.com/golang-jwt/jwt/v5 from 5.2.2 to 5.2.3 [[#283](https://github.com/opencloud-eu/reva/pull/283)]
- Bump go.etcd.io/etcd/client/v3 from 3.6.1 to 3.6.2 [[#281](https://github.com/opencloud-eu/reva/pull/281)]
- Bump golang.org/x/crypto from 0.39.0 to 0.40.0 [[#278](https://github.com/opencloud-eu/reva/pull/278)]
- Bump golang.org/x/sys from 0.33.0 to 0.34.0 [[#275](https://github.com/opencloud-eu/reva/pull/275)]
- Bump golang.org/x/sync from 0.15.0 to 0.16.0 [[#274](https://github.com/opencloud-eu/reva/pull/274)]
- Bump github.com/go-playground/validator/v10 from 10.26.0 to 10.27.0 [[#269](https://github.com/opencloud-eu/reva/pull/269)]
- Bump github.com/nats-io/nats-server/v2 from 2.11.5 to 2.11.6 [[#267](https://github.com/opencloud-eu/reva/pull/267)]
- Bump github.com/pkg/xattr from 0.4.11 to 0.4.12 [[#265](https://github.com/opencloud-eu/reva/pull/265)]

## [2.34.0](https://github.com/opencloud-eu/reva/releases/tag/v2.34.0) - 2025-06-27

### ❤️ Thanks to all contributors! ❤️

@aduffeck, @dragonchaser, @individual-it, @micbar, @rhafer

### ✨ Features

- feat: indicate if file has preview [[#214](https://github.com/opencloud-eu/reva/pull/214)]
- Add pending tasks to metrics endpoint [[#252](https://github.com/opencloud-eu/reva/pull/252)]
- Add rudimentary metrics for posixfs [[#251](https://github.com/opencloud-eu/reva/pull/251)]

### 🐛 Bug Fixes

- Use the parent to find the space id of items to be assimilated [[#255](https://github.com/opencloud-eu/reva/pull/255)]
- Do not try to read attributes from nodes with empty internal paths [[#257](https://github.com/opencloud-eu/reva/pull/257)]
- Fix handling of webdav write locks [[#233](https://github.com/opencloud-eu/reva/pull/233)]

### 📦️ Dependencies

- Bump go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc from 1.36.0 to 1.37.0 [[#263](https://github.com/opencloud-eu/reva/pull/263)]
- Bump github.com/nats-io/nats-server/v2 from 2.11.4 to 2.11.5 [[#262](https://github.com/opencloud-eu/reva/pull/262)]
- Bump go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc from 0.61.0 to 0.62.0 [[#259](https://github.com/opencloud-eu/reva/pull/259)]
- Bump github.com/go-chi/chi/v5 from 5.2.1 to 5.2.2 in the go_modules group [[#254](https://github.com/opencloud-eu/reva/pull/254)]
- Bump github.com/minio/minio-go/v7 from 7.0.93 to 7.0.94 [[#253](https://github.com/opencloud-eu/reva/pull/253)]
- Bump go.etcd.io/etcd/client/v3 from 3.6.0 to 3.6.1 [[#245](https://github.com/opencloud-eu/reva/pull/245)]
- Bump github.com/go-sql-driver/mysql from 1.9.2 to 1.9.3 [[#249](https://github.com/opencloud-eu/reva/pull/249)]
- Bump google.golang.org/grpc from 1.72.2 to 1.73.0 [[#243](https://github.com/opencloud-eu/reva/pull/243)]
- Bump github.com/minio/minio-go/v7 from 7.0.92 to 7.0.93 [[#244](https://github.com/opencloud-eu/reva/pull/244)]
- Bump github.com/cloudflare/circl from 1.3.7 to 1.6.1 in the go_modules group [[#239](https://github.com/opencloud-eu/reva/pull/239)]
- Bump github.com/ceph/go-ceph from 0.33.0 to 0.34.0 [[#241](https://github.com/opencloud-eu/reva/pull/241)]

## [2.33.1](https://github.com/opencloud-eu/reva/releases/tag/v2.33.1) - 2025-06-10

### ❤️ Thanks to all contributors! ❤️

@aduffeck, @rhafer

### 🐛 Bug Fixes

- Fix posixfs id cache after failed renames on disk [[#229](https://github.com/opencloud-eu/reva/pull/229)]
- Fix deadlock while copying metadata to CURRENT file [[#228](https://github.com/opencloud-eu/reva/pull/228)]

### 📦️ Dependencies

- Bump golang.org/x/crypto from 0.38.0 to 0.39.0 [[#236](https://github.com/opencloud-eu/reva/pull/236)]
- Bump github.com/pkg/xattr from 0.4.10 to 0.4.11 [[#237](https://github.com/opencloud-eu/reva/pull/237)]
- Bump golang.org/x/sync from 0.14.0 to 0.15.0 [[#235](https://github.com/opencloud-eu/reva/pull/235)]
- Bump golang.org/x/text from 0.25.0 to 0.26.0 [[#234](https://github.com/opencloud-eu/reva/pull/234)]
- Bump github.com/nats-io/nats.go from 1.42.0 to 1.43.0 [[#232](https://github.com/opencloud-eu/reva/pull/232)]
- Bump github.com/nats-io/nats-server/v2 from 2.11.3 to 2.11.4 [[#231](https://github.com/opencloud-eu/reva/pull/231)]
- Bump go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc from 1.35.0 to 1.36.0 [[#225](https://github.com/opencloud-eu/reva/pull/225)]
- Bump github.com/nats-io/nats.go from 1.41.2 to 1.42.0 [[#226](https://github.com/opencloud-eu/reva/pull/226)]
- Bump google.golang.org/grpc from 1.72.1 to 1.72.2 [[#224](https://github.com/opencloud-eu/reva/pull/224)]
- Bump github.com/mattn/go-sqlite3 from 1.14.27 to 1.14.28 [[#223](https://github.com/opencloud-eu/reva/pull/223)]
- Bump go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc from 0.60.0 to 0.61.0 [[#222](https://github.com/opencloud-eu/reva/pull/222)]
- Bump google.golang.org/grpc from 1.72.0 to 1.72.1 [[#220](https://github.com/opencloud-eu/reva/pull/220)]
- Bump go.etcd.io/etcd/client/v3 from 3.5.20 to 3.6.0 [[#219](https://github.com/opencloud-eu/reva/pull/219)]
- Bump github.com/nats-io/nats-server/v2 from 2.11.1 to 2.11.3 [[#204](https://github.com/opencloud-eu/reva/pull/204)]
- Bump github.com/minio/minio-go/v7 from 7.0.89 to 7.0.92 [[#218](https://github.com/opencloud-eu/reva/pull/218)]
- Bump github.com/aws/aws-sdk-go from 1.55.6 to 1.55.7 [[#215](https://github.com/opencloud-eu/reva/pull/215)]

## [2.33.0](https://github.com/opencloud-eu/reva/releases/tag/v2.33.0) - 2025-05-19

### ❤️ Thanks to all contributors! ❤️

@aduffeck, @rhafer

### 📈 Enhancement

- Add ParentID to events so it's available to the consumer [[#210](https://github.com/opencloud-eu/reva/pull/210)]

### 🐛 Bug Fixes

- Abort when the space root has already been created. [[#199](https://github.com/opencloud-eu/reva/pull/199)]
- Do not choke when one of the directory entries can't be read [[#196](https://github.com/opencloud-eu/reva/pull/196)]

### 📦️ Dependencies

- Bump github.com/segmentio/kafka-go from 0.4.47 to 0.4.48 [[#213](https://github.com/opencloud-eu/reva/pull/213)]
- Bump golang.org/x/term from 0.31.0 to 0.32.0 [[#211](https://github.com/opencloud-eu/reva/pull/211)]
- Bump golang.org/x/sys from 0.32.0 to 0.33.0 [[#209](https://github.com/opencloud-eu/reva/pull/209)]
- Bump golang.org/x/text from 0.24.0 to 0.25.0 [[#207](https://github.com/opencloud-eu/reva/pull/207)]
- Bump golang.org/x/oauth2 from 0.29.0 to 0.30.0 [[#206](https://github.com/opencloud-eu/reva/pull/206)]
- Bump github.com/beevik/etree from 1.5.0 to 1.5.1 [[#200](https://github.com/opencloud-eu/reva/pull/200)]
- Bump golang.org/x/oauth2 from 0.28.0 to 0.29.0 [[#201](https://github.com/opencloud-eu/reva/pull/201)]
- Bump github.com/onsi/gomega from 1.36.3 to 1.37.0 [[#194](https://github.com/opencloud-eu/reva/pull/194)]
- Bump github.com/onsi/ginkgo/v2 from 2.23.3 to 2.23.4 [[#198](https://github.com/opencloud-eu/reva/pull/198)]
- Bump github.com/go-ldap/ldap/v3 from 3.4.10 to 3.4.11 [[#195](https://github.com/opencloud-eu/reva/pull/195)]

## [2.32.0](https://github.com/opencloud-eu/reva/releases/tag/v2.32.0) - 2025-04-28

### ❤️ Thanks to all contributors! ❤️

@JammingBen, @aduffeck, @rhafer

### 🐛 Bug Fixes

- ocdav: Fix check for empty request body [[#188](https://github.com/opencloud-eu/reva/pull/188)]
- Fix space ids getting overwritten [[#178](https://github.com/opencloud-eu/reva/pull/178)]
- Improve performance and stabiity of assimilation [[#176](https://github.com/opencloud-eu/reva/pull/176)]
- Fix wrong blobsize attributes due to premature assimilation [[#172](https://github.com/opencloud-eu/reva/pull/172)]

### 📈 Enhancement

- Cephfs [[#180](https://github.com/opencloud-eu/reva/pull/180)]

### 📦️ Dependencies

- Bump google.golang.org/grpc from 1.71.1 to 1.72.0 [[#193](https://github.com/opencloud-eu/reva/pull/193)]
- Bump github.com/ceph/go-ceph from 0.32.0 to 0.33.0 [[#192](https://github.com/opencloud-eu/reva/pull/192)]
- Bump github.com/prometheus/client_golang from 1.21.1 to 1.22.0 [[#191](https://github.com/opencloud-eu/reva/pull/191)]
- Bump golang.org/x/sync from 0.12.0 to 0.13.0 [[#190](https://github.com/opencloud-eu/reva/pull/190)]
- Bump github.com/nats-io/nats.go from 1.41.1 to 1.41.2 [[#189](https://github.com/opencloud-eu/reva/pull/189)]
- Bump github.com/go-playground/validator/v10 from 10.25.0 to 10.26.0 [[#187](https://github.com/opencloud-eu/reva/pull/187)]
- Bump github.com/go-sql-driver/mysql from 1.9.1 to 1.9.2 [[#186](https://github.com/opencloud-eu/reva/pull/186)]
- Bump golang.org/x/net from 0.37.0 to 0.38.0 in the go_modules group [[#185](https://github.com/opencloud-eu/reva/pull/185)]
- Bump github.com/coreos/go-oidc/v3 from 3.13.0 to 3.14.1 [[#164](https://github.com/opencloud-eu/reva/pull/164)]
- Bump github.com/nats-io/nats-server/v2 from 2.11.0 to 2.11.1 in the go_modules group [[#182](https://github.com/opencloud-eu/reva/pull/182)]
- Bump golang.org/x/term from 0.30.0 to 0.31.0 [[#177](https://github.com/opencloud-eu/reva/pull/177)]
- Bump github.com/nats-io/nats.go from 1.41.0 to 1.41.1 [[#175](https://github.com/opencloud-eu/reva/pull/175)]
- Bump github.com/nats-io/nats.go from 1.39.1 to 1.41.0 [[#160](https://github.com/opencloud-eu/reva/pull/160)]

## [2.31.0](https://github.com/opencloud-eu/reva/releases/tag/v2.31.0) - 2025-04-07

### ❤️ Thanks to all contributors! ❤️

@JammingBen, @aduffeck, @individual-it, @openclouders

### ✨ Features

- revert: remove "edition" property [[#167](https://github.com/opencloud-eu/reva/pull/167)]

### 🐛 Bug Fixes

- Fix stale file metadata cache entries [[#166](https://github.com/opencloud-eu/reva/pull/166)]
- Do not send "delete" sses when items are moved [[#165](https://github.com/opencloud-eu/reva/pull/165)]

## [2.30.0](https://github.com/opencloud-eu/reva/releases/tag/v2.30.0) - 2025-04-04

### ❤️ Thanks to all contributors! ❤️

@aduffeck, @amrita-shrestha, @fschade, @rhafer

### 🐛 Bug Fixes

- Fix race condition when moving/restoring items in quick succession [[#158](https://github.com/opencloud-eu/reva/pull/158)]
- Fix handling collaborative moves [[#152](https://github.com/opencloud-eu/reva/pull/152)]
- Fix move races [[#150](https://github.com/opencloud-eu/reva/pull/150)]

### 📈 Enhancement

- Periodically log stats about inotify resources usage on the system [[#155](https://github.com/opencloud-eu/reva/pull/155)]
- Cache internal path and disabled flag [[#149](https://github.com/opencloud-eu/reva/pull/149)]
- enhancement(tus): Improve zerolog wrapper for slog [[#146](https://github.com/opencloud-eu/reva/pull/146)]

### 📦️ Dependencies

- Bump github.com/tus/tusd/v2 from 2.7.1 to 2.8.0 [[#159](https://github.com/opencloud-eu/reva/pull/159)]
- Bump github.com/mattn/go-sqlite3 from 1.14.24 to 1.14.27 [[#156](https://github.com/opencloud-eu/reva/pull/156)]
- Bump google.golang.org/grpc from 1.71.0 to 1.71.1 [[#154](https://github.com/opencloud-eu/reva/pull/154)]
- Bump github.com/minio/minio-go/v7 from 7.0.88 to 7.0.89 [[#151](https://github.com/opencloud-eu/reva/pull/151)]
- Bump google.golang.org/protobuf from 1.36.5 to 1.36.6 [[#142](https://github.com/opencloud-eu/reva/pull/142)]

## [2.29.1](https://github.com/opencloud-eu/reva/releases/tag/v2.29.1) - 2025-03-26

### ❤️ Thanks to all contributors! ❤️

@aduffeck, @amrita-shrestha

### 🐛 Bug Fixes

- Always try to get the blob id/size from the metadata [[#141](https://github.com/opencloud-eu/reva/pull/141)]

## [2.29.0](https://github.com/opencloud-eu/reva/releases/tag/v2.29.0) - 2025-03-26

### ❤️ Thanks to all contributors! ❤️

@JammingBen, @aduffeck, @amrita-shrestha, @butonic, @rhafer

### 🐛 Bug Fixes

- fix(appauth/jsoncs3) Avoid returing password hashes on API [[#139](https://github.com/opencloud-eu/reva/pull/139)]
- Make non-collaborative posix driver available on other OSes [[#132](https://github.com/opencloud-eu/reva/pull/132)]
- Fix moving lockfiles during moves [[#125](https://github.com/opencloud-eu/reva/pull/125)]
- Keep metadata lock files in .oc-nodes [[#120](https://github.com/opencloud-eu/reva/pull/120)]
- appauth/jsoncs3: Allow deletion using password hash [[#119](https://github.com/opencloud-eu/reva/pull/119)]
- Replace revisions with the exact same mtime [[#114](https://github.com/opencloud-eu/reva/pull/114)]
- Properly support disabling versioning [[#113](https://github.com/opencloud-eu/reva/pull/113)]
- Properly purge nodes depending on the storage type [[#108](https://github.com/opencloud-eu/reva/pull/108)]
- Fix traversing thrash items [[#106](https://github.com/opencloud-eu/reva/pull/106)]

### 📈 Enhancement

- New "jsoncs3" backend for app token storage [[#112](https://github.com/opencloud-eu/reva/pull/112)]

### 📦️ Dependencies

- Bump github.com/rs/zerolog from 1.33.0 to 1.34.0 [[#137](https://github.com/opencloud-eu/reva/pull/137)]
- Bump github.com/onsi/gomega from 1.36.2 to 1.36.3 [[#134](https://github.com/opencloud-eu/reva/pull/134)]
- Bump github.com/BurntSushi/toml from 1.4.0 to 1.5.0 [[#133](https://github.com/opencloud-eu/reva/pull/133)]
- Bump github.com/go-sql-driver/mysql from 1.9.0 to 1.9.1 [[#128](https://github.com/opencloud-eu/reva/pull/128)]
- Bump go.etcd.io/etcd/client/v3 from 3.5.19 to 3.5.20 [[#127](https://github.com/opencloud-eu/reva/pull/127)]
- Bump github.com/golang-jwt/jwt/v5 from 5.2.1 to 5.2.2 in the go_modules group [[#126](https://github.com/opencloud-eu/reva/pull/126)]
- Bump github.com/nats-io/nats-server/v2 from 2.10.26 to 2.11.0 [[#122](https://github.com/opencloud-eu/reva/pull/122)]
- Bump github.com/onsi/ginkgo/v2 from 2.23.1 to 2.23.2 [[#121](https://github.com/opencloud-eu/reva/pull/121)]
- Bump github.com/cheggaaa/pb/v3 from 3.1.6 to 3.1.7 [[#117](https://github.com/opencloud-eu/reva/pull/117)]
- Bump github.com/onsi/ginkgo/v2 from 2.23.0 to 2.23.1 [[#116](https://github.com/opencloud-eu/reva/pull/116)]
- Bump github.com/minio/minio-go/v7 from 7.0.87 to 7.0.88 [[#111](https://github.com/opencloud-eu/reva/pull/111)]
- Bump github.com/opencloud-eu/opencloud from 1.0.0 to 1.1.0 [[#110](https://github.com/opencloud-eu/reva/pull/110)]
- Bump github.com/shamaton/msgpack/v2 from 2.2.2 to 2.2.3 [[#103](https://github.com/opencloud-eu/reva/pull/103)]
- Bump github.com/coreos/go-oidc/v3 from 3.12.0 to 3.13.0 [[#104](https://github.com/opencloud-eu/reva/pull/104)]

## [2.28.0](https://github.com/opencloud-eu/reva/releases/tag/v2.28.0) - 2025-03-17

### ❤️ Thanks to all contributors! ❤️

@S-Panta, @aduffeck, @butonic, @kulmann, @micbar, @rhafer

### 📚 Documentation

- feat: add ready release go [[#101](https://github.com/opencloud-eu/reva/pull/101)]

### 🐛 Bug Fixes

- Use lowercase space aliases by default [[#95](https://github.com/opencloud-eu/reva/pull/95)]
- Trash fixes [[#96](https://github.com/opencloud-eu/reva/pull/96)]
- Handle invalid restore requests [[#94](https://github.com/opencloud-eu/reva/pull/94)]
- Warmup the id cache for restored folder [[#93](https://github.com/opencloud-eu/reva/pull/93)]
- Make sure to cleanup properly after tests [[#90](https://github.com/opencloud-eu/reva/pull/90)]
- Handle moved items properly, be more robust during assimilation [[#88](https://github.com/opencloud-eu/reva/pull/88)]
- Fix wrong entries in the space indexes in posixfs [[#77](https://github.com/opencloud-eu/reva/pull/77)]
- fix lint for golangci-lint v1.64.6 [[#83](https://github.com/opencloud-eu/reva/pull/83)]
- Make sure to return a node with a space root [[#82](https://github.com/opencloud-eu/reva/pull/82)]
- Fix mocks and drop bingo in favor of `go tool` [[#78](https://github.com/opencloud-eu/reva/pull/78)]
- always propagate size diff on trash restore [[#76](https://github.com/opencloud-eu/reva/pull/76)]
- Fix finding spaces while warming up the id cache [[#74](https://github.com/opencloud-eu/reva/pull/74)]
- posixfs: check trash permissions [[#67](https://github.com/opencloud-eu/reva/pull/67)]
- posixfs: fix cache warmup [[#68](https://github.com/opencloud-eu/reva/pull/68)]
- Properly clean up lockfiles after propagation [[#60](https://github.com/opencloud-eu/reva/pull/60)]
- Fix restoring files from the trashbin [[#63](https://github.com/opencloud-eu/reva/pull/63)]
- posix: invalidate old cache on move [[#61](https://github.com/opencloud-eu/reva/pull/61)]
- Use the correct folder for trash on decomposed fs [[#57](https://github.com/opencloud-eu/reva/pull/57)]
- posix: invalidate cache for deleted spaces [[#56](https://github.com/opencloud-eu/reva/pull/56)]
- use node to get space root path [[#53](https://github.com/opencloud-eu/reva/pull/53)]
- Fix failing trash tests [[#49](https://github.com/opencloud-eu/reva/pull/49)]
- remove unused metadata List() [[#48](https://github.com/opencloud-eu/reva/pull/48)]
- Fix handling deletions [[#47](https://github.com/opencloud-eu/reva/pull/47)]
- Fix integration tests after recent refactoring [[#42](https://github.com/opencloud-eu/reva/pull/42)]
- Fix revisions [[#40](https://github.com/opencloud-eu/reva/pull/40)]
- drop dash in decomposeds3 driver name [[#39](https://github.com/opencloud-eu/reva/pull/39)]
- Register decomposed_s3 with the proper name [[#38](https://github.com/opencloud-eu/reva/pull/38)]
- Assimilate mtime [[#27](https://github.com/opencloud-eu/reva/pull/27)]
- Remove some unused command completions [[#20](https://github.com/opencloud-eu/reva/pull/20)]
- renames: incorporating review feedback [[#16](https://github.com/opencloud-eu/reva/pull/16)]
- Fix branch name in expected failure files [[#12](https://github.com/opencloud-eu/reva/pull/12)]
- Fix references in expected failures files [[#10](https://github.com/opencloud-eu/reva/pull/10)]
- Align depend-a-bot config with main repo [[#8](https://github.com/opencloud-eu/reva/pull/8)]
- Cleanup xattr names [[#4](https://github.com/opencloud-eu/reva/pull/4)]
- Fix LDAP related tests [[#6](https://github.com/opencloud-eu/reva/pull/6)]
- Fix remaining unit test failure [[#5](https://github.com/opencloud-eu/reva/pull/5)]
- Fix build and cleanup [[#3](https://github.com/opencloud-eu/reva/pull/3)]

### 📈 Enhancement

- Benchmark fs [[#62](https://github.com/opencloud-eu/reva/pull/62)]
- Add support for the hybrid backend to the test helpers [[#50](https://github.com/opencloud-eu/reva/pull/50)]
- Add a hybrid metadatabackend [[#41](https://github.com/opencloud-eu/reva/pull/41)]
- Message pack metadata v2 [[#32](https://github.com/opencloud-eu/reva/pull/32)]
- Add option to disable the posix fs watcher [[#25](https://github.com/opencloud-eu/reva/pull/25)]
- Implement revisions for the posix fs [[#11](https://github.com/opencloud-eu/reva/pull/11)]
- Use a different kafka group id [[#15](https://github.com/opencloud-eu/reva/pull/15)]
- Add 'decomposed' and 'decomposed_s3' storage drivers [[#2](https://github.com/opencloud-eu/reva/pull/2)]

### 📦️ Dependency

- Bump go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc from 0.59.0 to 0.60.0 [[#99](https://github.com/opencloud-eu/reva/pull/99)]
- Bump github.com/onsi/ginkgo/v2 from 2.22.2 to 2.23.0 [[#100](https://github.com/opencloud-eu/reva/pull/100)]
- Bump go.opentelemetry.io/otel/sdk from 1.34.0 to 1.35.0 [[#98](https://github.com/opencloud-eu/reva/pull/98)]
- Bump github.com/go-chi/chi/v5 from 5.2.0 to 5.2.1 [[#97](https://github.com/opencloud-eu/reva/pull/97)]
- Bump github.com/prometheus/alertmanager from 0.27.0 to 0.28.1 [[#91](https://github.com/opencloud-eu/reva/pull/91)]
- Bump go.opentelemetry.io/otel/trace from 1.34.0 to 1.35.0 [[#92](https://github.com/opencloud-eu/reva/pull/92)]
- Bump go.etcd.io/etcd/client/v3 from 3.5.18 to 3.5.19 [[#87](https://github.com/opencloud-eu/reva/pull/87)]
- Bump github.com/go-playground/validator/v10 from 10.23.0 to 10.25.0 [[#86](https://github.com/opencloud-eu/reva/pull/86)]
- Bump github.com/tus/tusd/v2 from 2.6.0 to 2.7.1 [[#79](https://github.com/opencloud-eu/reva/pull/79)]
- Bump google.golang.org/grpc from 1.70.0 to 1.71.0 [[#80](https://github.com/opencloud-eu/reva/pull/80)]
- Bump github.com/prometheus/client_golang from 1.21.0 to 1.21.1 [[#72](https://github.com/opencloud-eu/reva/pull/72)]
- Bump golang.org/x/oauth2 from 0.27.0 to 0.28.0 [[#71](https://github.com/opencloud-eu/reva/pull/71)]
- Bump github.com/google/go-cmp from 0.6.0 to 0.7.0 [[#70](https://github.com/opencloud-eu/reva/pull/70)]
- Bump golang.org/x/text from 0.22.0 to 0.23.0 [[#69](https://github.com/opencloud-eu/reva/pull/69)]
- Bump golang.org/x/oauth2 from 0.26.0 to 0.27.0 [[#65](https://github.com/opencloud-eu/reva/pull/65)]
- Bump google.golang.org/protobuf from 1.36.3 to 1.36.5 [[#64](https://github.com/opencloud-eu/reva/pull/64)]
- Bump github.com/opencloud-eu/opencloud from 0.0.0-20250128123102-82fa07c003f4 to 1.0.0 [[#59](https://github.com/opencloud-eu/reva/pull/59)]
- Bump google.golang.org/grpc from 1.69.4 to 1.70.0 [[#58](https://github.com/opencloud-eu/reva/pull/58)]
- Bump github.com/go-sql-driver/mysql from 1.8.1 to 1.9.0 [[#55](https://github.com/opencloud-eu/reva/pull/55)]
- Bump github.com/nats-io/nats-server/v2 from 2.10.24 to 2.10.26 [[#54](https://github.com/opencloud-eu/reva/pull/54)]
- Bump github.com/rogpeppe/go-internal from 1.13.1 to 1.14.1 [[#51](https://github.com/opencloud-eu/reva/pull/51)]
- Bump github.com/minio/minio-go/v7 from 7.0.78 to 7.0.87 [[#52](https://github.com/opencloud-eu/reva/pull/52)]
- Bump github.com/go-jose/go-jose/v4 from 4.0.2 to 4.0.5 in the go_modules group [[#45](https://github.com/opencloud-eu/reva/pull/45)]
- Bump github.com/cheggaaa/pb from 1.0.29 to 2.0.7+incompatible [[#46](https://github.com/opencloud-eu/reva/pull/46)]
- Bump github.com/prometheus/client_golang from 1.20.5 to 1.21.0 [[#44](https://github.com/opencloud-eu/reva/pull/44)]
- Bump github.com/nats-io/nats.go from 1.38.0 to 1.39.1 [[#43](https://github.com/opencloud-eu/reva/pull/43)]
- Bump github.com/ceph/go-ceph from 0.30.0 to 0.32.0 [[#37](https://github.com/opencloud-eu/reva/pull/37)]
- Bump go.etcd.io/etcd/client/v3 from 3.5.16 to 3.5.18 [[#36](https://github.com/opencloud-eu/reva/pull/36)]
- Bump github.com/go-ldap/ldap/v3 from 3.4.8 to 3.4.10 [[#34](https://github.com/opencloud-eu/reva/pull/34)]
- Bump github.com/aws/aws-sdk-go from 1.55.5 to 1.55.6 [[#33](https://github.com/opencloud-eu/reva/pull/33)]
- Bump github.com/beevik/etree from 1.4.1 to 1.5.0 [[#30](https://github.com/opencloud-eu/reva/pull/30)]
- Bump golang.org/x/crypto from 0.32.0 to 0.33.0 [[#29](https://github.com/opencloud-eu/reva/pull/29)]
- Bump github.com/hashicorp/go-plugin from 1.6.2 to 1.6.3 [[#24](https://github.com/opencloud-eu/reva/pull/24)]
- Bump golang.org/x/oauth2 from 0.25.0 to 0.26.0 [[#23](https://github.com/opencloud-eu/reva/pull/23)]
- Bump inotifywaitgo to 0.0.9 [[#22](https://github.com/opencloud-eu/reva/pull/22)]
- Bump go-git to 5.13.0 (CVE-2025-21613) [[#19](https://github.com/opencloud-eu/reva/pull/19)]
