# Changelog

## [2.28.1](https://github.com/opencloud-eu/reva/releases/tag/v2.28.1) - 2025-03-18

### ❤️ Thanks to all contributors! ❤️

@JammingBen, @aduffeck

### 🐛 Bug Fixes

- Properly purge nodes depending on the storage type [[#108](https://github.com/opencloud-eu/reva/pull/108)]
- Fix traversing thrash items [[#106](https://github.com/opencloud-eu/reva/pull/106)]

### 📦️ Dependency

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
