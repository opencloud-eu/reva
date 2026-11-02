// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package tree

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rs/zerolog"

	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/ignore"
	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/options"
	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/watcher"
	decomposedoptions "github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/options"
)

var _ = Describe("CephFSWatcher", func() {
	var w *CephFSWatcher

	BeforeEach(func() {
		logger := zerolog.Nop()
		o := &options.Options{
			Options: decomposedoptions.Options{
				Root:                      "/storage",
				PersonalSpacePathTemplate: "users/{{.User.Id.OpaqueId}}",
				GeneralSpacePathTemplate:  "spaces/{{.Space.Id.OpaqueId}}",
				UploadDirectory:           "/storage/uploads",
			},
			WatchRoot: "/storage",
		}
		t := &Tree{options: o, Ignorer: ignore.NewIgnorer(o, &logger)}
		var err error
		w, err = NewCephfsWatcher(t, []string{"localhost:9092"}, &logger)
		Expect(err).ToNot(HaveOccurred())
	})

	// the records are the ones the MDS emits, see ChangeNotifyFormat.h in ceph
	DescribeTable("scans",
		func(record string, want []cephfsScan) {
			ev := &cephfsEvent{}
			Expect(json.Unmarshal([]byte(record), ev)).To(Succeed())
			Expect(w.scans(ev)).To(Equal(want))
		},
		Entry("create file", `{"mask": 16, "path": "users/u1/a.txt"}`,
			[]cephfsScan{{"/storage/users/u1/a.txt", watcher.ActionCreate, false}}),
		Entry("create dir", `{"mask": 65552, "path": "users/u1/d"}`,
			[]cephfsScan{{"/storage/users/u1/d", watcher.ActionCreate, true}}),
		Entry("write flush", `{"mask": 4, "path": "users/u1/a.txt"}`,
			[]cephfsScan{{"/storage/users/u1/a.txt", watcher.ActionUpdate, false}}),
		Entry("delete file", `{"mask": 32, "path": "users/u1/a.txt"}`,
			[]cephfsScan{{"/storage/users/u1/a.txt", watcher.ActionDelete, false}}),
		Entry("delete dir", `{"mask": 65568, "path": "users/u1/d"}`,
			[]cephfsScan{{"/storage/users/u1/d", watcher.ActionDelete, true}}),
		Entry("move", `{"mask": 0, "path": "", "src_mask": 512, "src_path": "users/u1/a.txt", "dest_mask": 1024, "dest_path": "users/u1/b.txt"}`,
			[]cephfsScan{
				{"/storage/users/u1/a.txt", watcher.ActionMoveFrom, false},
				{"/storage/users/u1/b.txt", watcher.ActionMove, false},
			}),
		Entry("move dir", `{"mask": 0, "path": "", "src_mask": 66048, "src_path": "users/u1/d", "dest_mask": 66560, "dest_path": "users/u1/e"}`,
			[]cephfsScan{
				{"/storage/users/u1/d", watcher.ActionMoveFrom, true},
				{"/storage/users/u1/e", watcher.ActionMove, true},
			}),
		Entry("move in", `{"mask": 0, "path": "", "dest_mask": 1024, "dest_path": "users/u1/b.txt"}`,
			[]cephfsScan{{"/storage/users/u1/b.txt", watcher.ActionMove, false}}),

		Entry("ignores metadata", `{"mask": 16, "path": "users/u1/.oc-nodes/x"}`, nil),
		Entry("ignores tmp", `{"mask": 4, "path": "users/u1/.oc-tmp/a.txt"}`, nil),
		Entry("ignores uploads", `{"mask": 16, "path": "uploads/abc"}`, nil),
		Entry("ignores lock files", `{"mask": 16, "path": "users/u1/a.txt.mlock"}`, nil),
		Entry("move from tmp only scans the destination", `{"mask": 0, "path": "", "src_mask": 512, "src_path": "users/u1/.oc-tmp/a.txt", "dest_mask": 1024, "dest_path": "users/u1/a.txt"}`,
			[]cephfsScan{{"/storage/users/u1/a.txt", watcher.ActionMove, false}}),
		Entry("move into an ignored dir only scans the source", `{"mask": 0, "path": "", "src_mask": 512, "src_path": "users/u1/a.txt", "dest_mask": 1024, "dest_path": "uploads/a.txt"}`,
			[]cephfsScan{{"/storage/users/u1/a.txt", watcher.ActionMoveFrom, false}}),
		Entry("ignores close", `{"mask": 4096, "path": "users/u1/a.txt"}`, nil),
	)

	Describe("newKafkaDialer", func() {
		It("has no tls or sasl by default", func() {
			d, err := newKafkaDialer(options.KafkaWatcherConfig{})
			Expect(err).ToNot(HaveOccurred())
			Expect(d.TLS).To(BeNil())
			Expect(d.SASLMechanism).To(BeNil())
		})

		It("sets up tls and sasl", func() {
			d, err := newKafkaDialer(options.KafkaWatcherConfig{
				EnableTLS:     true,
				SASLMechanism: "SCRAM-SHA-512",
				Username:      "u",
				Password:      "p",
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(d.TLS).ToNot(BeNil())
			Expect(d.SASLMechanism.Name()).To(Equal("SCRAM-SHA-512"))
		})

		It("rejects unknown sasl mechanisms", func() {
			_, err := newKafkaDialer(options.KafkaWatcherConfig{SASLMechanism: "GSSAPI"})
			Expect(err).To(HaveOccurred())
		})
	})
})
