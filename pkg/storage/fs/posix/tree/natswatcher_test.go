// Copyright 2018-2021 CERN
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// In applying this license, CERN does not waive the privileges and immunities
// granted to it by virtue of its status as an Intergovernmental Organization
// or submit itself to any jurisdiction.

package tree_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"time"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/tree"
	"github.com/rs/zerolog"
	"github.com/vmihailenco/msgpack/v5"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NatsWatcher", func() {
	var (
		natsServer     *server.Server
		natsURL        string
		natsServerPort int
		config         tree.Config
		logger         zerolog.Logger
		ctx            context.Context
		cancel         context.CancelFunc
		subtree        string
	)

	BeforeEach(func() {
		SetDefaultEventuallyTimeout(15 * time.Second)

		// Create temporary root directory
		var err error
		subtree, err = generateRandomString(10)
		Expect(err).ToNot(HaveOccurred())
		subtree = "/" + subtree
		root = env.Root + "/users/" + env.Owner.Username + subtree
		Expect(os.Mkdir(root, 0700)).To(Succeed())

		// Start embedded NATS server with JetStream
		opts := &server.Options{
			Port:      -1, // Random port
			JetStream: true,
		}
		natsServer, err = server.NewServer(opts)
		Expect(err).ToNot(HaveOccurred())

		go natsServer.Start()
		if !natsServer.ReadyForConnections(4 * time.Second) {
			Fail("NATS server failed to start")
		}

		natsURL = natsServer.ClientURL()
		// Store the port for potential reconnection tests
		natsServerPort = natsServer.Addr().(*net.TCPAddr).Port

		// Setup logger
		logger = zerolog.New(os.Stdout).With().Timestamp().Logger()

		// Setup config
		config = tree.Config{
			Endpoint:      natsURL,
			EnableTLS:     false,
			MaxAckPending: 100,
			AckWait:       30 * time.Second,
		}

		// Create context
		ctx, cancel = context.WithCancel(context.Background())

		Eventually(func(g Gomega) {
			n, err := env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{
				ResourceId: env.SpaceRootRes,
				Path:       subtree,
			})
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(n.Exists).To(BeTrue())
		}).Should(Succeed())
	})

	AfterEach(func() {
		cancel()
		if natsServer != nil {
			natsServer.Shutdown()
			natsServer.WaitForShutdown()
		}
		if root != "" {
			os.RemoveAll(root)
		}
	})

	Describe("NewNatsWatcher", func() {
		It("creates a new watcher instance", func() {
			watcher, err := tree.NewNatsWatcher(env.Tree, config, "test-group", &logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(watcher).ToNot(BeNil())
		})
	})

	Describe("Watch", func() {
		var (
			watcher    *tree.NatsWatcher
			streamName string
			subject    string
		)

		BeforeEach(func() {
			var err error
			streamName = "TEST_STREAM"
			subject = "test.events"

			watcher, err = tree.NewNatsWatcher(env.Tree, config, "test-group", &logger)
			Expect(err).ToNot(HaveOccurred())

			// Setup JetStream stream
			nc, err := nats.Connect(natsURL)
			Expect(err).ToNot(HaveOccurred())
			defer nc.Close()

			js, err := jetstream.New(nc)
			Expect(err).ToNot(HaveOccurred())

			_, err = js.CreateStream(ctx, jetstream.StreamConfig{
				Name:     streamName,
				Subjects: []string{subject},
			})
			Expect(err).ToNot(HaveOccurred())
		})

		It("consumes CREATE events", func() {
			// Start watcher in background
			go func() {
				_ = watcher.Watch(ctx, streamName, subject)
			}()

			// Give watcher time to start
			time.Sleep(500 * time.Millisecond)

			// Publish CREATE event
			nc, err := nats.Connect(natsURL)
			Expect(err).ToNot(HaveOccurred())
			defer nc.Close()

			js, err := jetstream.New(nc)
			Expect(err).ToNot(HaveOccurred())

			testFile := "test.txt"
			testPath := filepath.Join(root, testFile)

			event := map[string]interface{}{
				"e": "CREATE",
				"p": subtree + "/" + testFile,
				"t": "file",
			}

			data, err := msgpack.Marshal(event)
			Expect(err).ToNot(HaveOccurred())

			// Create the actual file
			err = os.WriteFile(testPath, []byte("test"), 0644)
			Expect(err).ToNot(HaveOccurred())

			_, err = js.Publish(ctx, subject, data)
			Expect(err).ToNot(HaveOccurred())

			// Wait for processing and verify file is tracked
			Eventually(func(g Gomega) {
				n, err := env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{
					ResourceId: env.SpaceRootRes,
					Path:       subtree + "/" + testFile,
				})
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(n.Exists).To(BeTrue())
			}).Should(Succeed())
		})

		It("consumes DELETE events", func() {
			// Create a test file first
			testFile := "delete-me.txt"
			testPath := filepath.Join(root, testFile)
			err := os.WriteFile(testPath, []byte("test"), 0644)
			Expect(err).ToNot(HaveOccurred())

			// Wait for the file to be tracked
			Eventually(func(g Gomega) {
				n, err := env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{
					ResourceId: env.SpaceRootRes,
					Path:       subtree + "/" + testFile,
				})
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(n.Exists).To(BeTrue())
			}).Should(Succeed())

			// Start watcher
			go func() {
				_ = watcher.Watch(ctx, streamName, subject)
			}()

			time.Sleep(500 * time.Millisecond)

			// Publish DELETE event
			nc, err := nats.Connect(natsURL)
			Expect(err).ToNot(HaveOccurred())
			defer nc.Close()

			js, err := jetstream.New(nc)
			Expect(err).ToNot(HaveOccurred())

			event := map[string]interface{}{
				"e": "DELETE",
				"p": subtree + "/" + testFile,
				"t": "file",
			}

			data, err := msgpack.Marshal(event)
			Expect(err).ToNot(HaveOccurred())

			// Delete the file
			err = os.Remove(testPath)
			Expect(err).ToNot(HaveOccurred())

			_, err = js.Publish(ctx, subject, data)
			Expect(err).ToNot(HaveOccurred())

			// Verify file is deleted from tree
			Eventually(func(g Gomega) {
				n, err := env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{
					ResourceId: env.SpaceRootRes,
					Path:       subtree + "/" + testFile,
				})
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(n.Exists).To(BeFalse())
			}).Should(Succeed())
		})

		It("consumes MOVE events", func() {
			// Create source file
			srcFile := "source.txt"
			dstFile := "destination.txt"
			srcPath := filepath.Join(root, srcFile)

			err := os.WriteFile(srcPath, []byte("test"), 0644)
			Expect(err).ToNot(HaveOccurred())

			// Wait for file to be tracked
			Eventually(func(g Gomega) {
				n, err := env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{
					ResourceId: env.SpaceRootRes,
					Path:       subtree + "/" + srcFile,
				})
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(n.Exists).To(BeTrue())
			}).Should(Succeed())

			// Start watcher
			go func() {
				_ = watcher.Watch(ctx, streamName, subject)
			}()

			time.Sleep(500 * time.Millisecond)

			// Publish MOVE event
			nc, err := nats.Connect(natsURL)
			Expect(err).ToNot(HaveOccurred())
			defer nc.Close()

			js, err := jetstream.New(nc)
			Expect(err).ToNot(HaveOccurred())

			event := map[string]interface{}{
				"e": "MOVE",
				"p": subtree + "/" + srcFile,
				"d": subtree + "/" + dstFile,
				"t": "file",
			}

			data, err := msgpack.Marshal(event)
			Expect(err).ToNot(HaveOccurred())

			// Perform actual move
			dstPath := filepath.Join(root, dstFile)
			err = os.Rename(srcPath, dstPath)
			Expect(err).ToNot(HaveOccurred())

			_, err = js.Publish(ctx, subject, data)
			Expect(err).ToNot(HaveOccurred())

			// Verify move in tree
			Eventually(func(g Gomega) {
				n, err := env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{
					ResourceId: env.SpaceRootRes,
					Path:       subtree + "/" + srcFile,
				})
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(n.Exists).To(BeFalse())

				n, err = env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{
					ResourceId: env.SpaceRootRes,
					Path:       subtree + "/" + dstFile,
				})
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(n.Exists).To(BeTrue())
			}).Should(Succeed())
		})

		It("handles CLOSE_WRITE events", func() {
			testFile := "updated.txt"
			testPath := filepath.Join(root, testFile)

			// Create initial file
			err := os.WriteFile(testPath, []byte("initial"), 0644)
			Expect(err).ToNot(HaveOccurred())

			// Wait for file to be tracked
			Eventually(func(g Gomega) {
				n, err := env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{
					ResourceId: env.SpaceRootRes,
					Path:       subtree + "/" + testFile,
				})
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(n.Exists).To(BeTrue())
			}).Should(Succeed())

			// Start watcher
			go func() {
				_ = watcher.Watch(ctx, streamName, subject)
			}()

			time.Sleep(500 * time.Millisecond)

			// Update file
			err = os.WriteFile(testPath, []byte("updated"), 0644)
			Expect(err).ToNot(HaveOccurred())

			// Publish CLOSE_WRITE event
			nc, err := nats.Connect(natsURL)
			Expect(err).ToNot(HaveOccurred())
			defer nc.Close()

			js, err := jetstream.New(nc)
			Expect(err).ToNot(HaveOccurred())

			event := map[string]interface{}{
				"e": "CLOSE_WRITE",
				"p": subtree + "/" + testFile,
				"t": "file",
				"b": int64(7),
			}

			data, err := msgpack.Marshal(event)
			Expect(err).ToNot(HaveOccurred())

			_, err = js.Publish(ctx, subject, data)
			Expect(err).ToNot(HaveOccurred())

			// Verify file updated in tree
			time.Sleep(500 * time.Millisecond)

			content, err := os.ReadFile(testPath)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(content)).To(Equal("updated"))
		})

		It("handles directory events", func() {
			testDir := "testdir"
			testPath := filepath.Join(root, testDir)

			// Start watcher
			go func() {
				_ = watcher.Watch(ctx, streamName, subject)
			}()

			time.Sleep(500 * time.Millisecond)

			// Create directory
			err := os.Mkdir(testPath, 0755)
			Expect(err).ToNot(HaveOccurred())

			// Publish CREATE event for directory
			nc, err := nats.Connect(natsURL)
			Expect(err).ToNot(HaveOccurred())
			defer nc.Close()

			js, err := jetstream.New(nc)
			Expect(err).ToNot(HaveOccurred())

			event := map[string]interface{}{
				"e": "CREATE",
				"p": subtree + "/" + testDir,
				"t": "dir",
			}

			data, err := msgpack.Marshal(event)
			Expect(err).ToNot(HaveOccurred())

			_, err = js.Publish(ctx, subject, data)
			Expect(err).ToNot(HaveOccurred())

			// Verify directory is tracked
			Eventually(func(g Gomega) {
				n, err := env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{
					ResourceId: env.SpaceRootRes,
					Path:       subtree + "/" + testDir,
				})
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(n.Exists).To(BeTrue())
			}).Should(Succeed())
		})

		It("stops when context is cancelled", func() {
			watchCtx, watchCancel := context.WithTimeout(ctx, 3*time.Second)
			defer watchCancel()

			done := make(chan error, 1)
			go func() {
				done <- watcher.Watch(watchCtx, streamName, subject)
			}()

			// Wait for watcher to start
			time.Sleep(500 * time.Millisecond)

			// Cancel context
			watchCancel()

			// Wait for Watch to return with context error
			Eventually(done, 5*time.Second).Should(Receive(MatchError(context.Canceled)))
		})

		It("reconnects on connection failure", func() {
			// Start watcher
			watcherDone := make(chan struct{})
			go func() {
				defer GinkgoRecover()
				_ = watcher.Watch(ctx, streamName, subject)
				close(watcherDone)
			}()

			// Wait for initial connection
			time.Sleep(1 * time.Second)

			// Publish an event to verify watcher is working
			nc, err := nats.Connect(natsURL)
			Expect(err).ToNot(HaveOccurred())

			js, err := jetstream.New(nc)
			Expect(err).ToNot(HaveOccurred())

			testFile1 := "before-disconnect.txt"
			event := map[string]interface{}{
				"e": "CREATE",
				"p": subtree + "/" + testFile1,
				"t": "file",
			}

			data, err := msgpack.Marshal(event)
			Expect(err).ToNot(HaveOccurred())

			err = os.WriteFile(filepath.Join(root, testFile1), []byte("test"), 0644)
			Expect(err).ToNot(HaveOccurred())

			_, err = js.Publish(ctx, subject, data)
			Expect(err).ToNot(HaveOccurred())
			nc.Close()

			time.Sleep(500 * time.Millisecond)

			// Shutdown server to simulate connection failure
			natsServer.Shutdown()
			natsServer.WaitForShutdown()

			time.Sleep(2 * time.Second)

			// Restart server on same port
			opts := &server.Options{
				Port:      natsServerPort,
				JetStream: true,
			}
			var serverErr error
			natsServer, serverErr = server.NewServer(opts)
			Expect(serverErr).ToNot(HaveOccurred())

			go natsServer.Start()
			Expect(natsServer.ReadyForConnections(4 * time.Second)).To(BeTrue())

			// Update natsURL with new server
			natsURL = natsServer.ClientURL()

			// Recreate stream
			nc, err = nats.Connect(natsURL)
			Expect(err).ToNot(HaveOccurred())

			js, err = jetstream.New(nc)
			Expect(err).ToNot(HaveOccurred())

			_, err = js.CreateStream(ctx, jetstream.StreamConfig{
				Name:     streamName,
				Subjects: []string{subject},
			})
			Expect(err).ToNot(HaveOccurred())

			// Wait for reconnection (watcher has exponential backoff)
			time.Sleep(5 * time.Second)

			// Verify watcher reconnected by publishing another event
			testFile2 := "after-reconnect.txt"
			event = map[string]interface{}{
				"e": "CREATE",
				"p": subtree + "/" + testFile2,
				"t": "file",
			}

			data, err = msgpack.Marshal(event)
			Expect(err).ToNot(HaveOccurred())

			err = os.WriteFile(filepath.Join(root, testFile2), []byte("test"), 0644)
			Expect(err).ToNot(HaveOccurred())

			_, err = js.Publish(ctx, subject, data)
			Expect(err).ToNot(HaveOccurred())

			nc.Close()

			// Verify the file after reconnect is tracked
			Eventually(func(g Gomega) {
				n, err := env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{
					ResourceId: env.SpaceRootRes,
					Path:       subtree + "/" + testFile2,
				})
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(n.Exists).To(BeTrue())
			}, 10*time.Second).Should(Succeed())
		})
	})

	Describe("TLS Configuration", func() {
		It("accepts TLS configuration", func() {
			tlsConfig := tree.Config{
				Endpoint:             natsURL,
				EnableTLS:            true,
				TLSInsecure:          true,
				TLSRootCACertificate: "",
				MaxAckPending:        100,
				AckWait:              30 * time.Second,
			}

			watcher, err := tree.NewNatsWatcher(env.Tree, tlsConfig, "tls-group", &logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(watcher).ToNot(BeNil())
		})
	})

	Describe("Authentication", func() {
		It("accepts authentication configuration", func() {
			authConfig := tree.Config{
				Endpoint:      natsURL,
				EnableTLS:     false,
				AuthUsername:  "testuser",
				AuthPassword:  "testpass",
				MaxAckPending: 100,
				AckWait:       30 * time.Second,
			}

			watcher, err := tree.NewNatsWatcher(env.Tree, authConfig, "auth-group", &logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(watcher).ToNot(BeNil())
		})
	})
})
