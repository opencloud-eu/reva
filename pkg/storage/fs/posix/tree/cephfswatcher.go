// Copyright 2025 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package tree

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/options"
	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/watcher"
	"github.com/rs/zerolog"
	kafka "github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl"
	"github.com/segmentio/kafka-go/sasl/plain"
	"github.com/segmentio/kafka-go/sasl/scram"
)

const (
	CEPH_MDS_NOTIFY_ACCESS        = 0x0000000000000001 // 1
	CEPH_MDS_NOTIFY_ATTRIB        = 0x0000000000000002 // 2
	CEPH_MDS_NOTIFY_CLOSE_WRITE   = 0x0000000000000004 // 4
	CEPH_MDS_NOTIFY_CLOSE_NOWRITE = 0x0000000000000008 // 8
	CEPH_MDS_NOTIFY_CREATE        = 0x0000000000000010 // 16
	CEPH_MDS_NOTIFY_DELETE        = 0x0000000000000020 // 32
	CEPH_MDS_NOTIFY_DELETE_SELF   = 0x0000000000000040 // 64
	CEPH_MDS_NOTIFY_MODIFY        = 0x0000000000000080 // 128
	CEPH_MDS_NOTIFY_MOVE_SELF     = 0x0000000000000100 // 256
	CEPH_MDS_NOTIFY_MOVED_FROM    = 0x0000000000000200 // 512
	CEPH_MDS_NOTIFY_MOVED_TO      = 0x0000000000000400 // 1024
	CEPH_MDS_NOTIFY_OPEN          = 0x0000000000000800 // 2048
	CEPH_MDS_NOTIFY_CLOSE         = 0x0000000000001000 // 4096
	CEPH_MDS_NOTIFY_MOVE          = 0x0000000000002000 // 8192
	CEPH_MDS_NOTIFY_ONESHOT       = 0x0000000000004000 // 16384
	CEPH_MDS_NOTIFY_IGNORED       = 0x0000000000008000 // 32768
	CEPH_MDS_NOTIFY_ONLYDIR       = 0x0000000000010000 // 65536
)

type CephFSWatcher struct {
	tree    *Tree
	root    string
	brokers []string
	dialer  *kafka.Dialer
	log     *zerolog.Logger
}

func NewCephfsWatcher(tree *Tree, brokers []string, log *zerolog.Logger) (*CephFSWatcher, error) {
	dialer, err := newKafkaDialer(tree.options.KafkaWatcher)
	if err != nil {
		return nil, err
	}
	return &CephFSWatcher{
		tree:    tree,
		root:    tree.options.WatchRoot,
		brokers: brokers,
		dialer:  dialer,
		log:     log,
	}, nil
}

// newKafkaDialer returns a dialer with the configured TLS and SASL settings.
func newKafkaDialer(c options.KafkaWatcherConfig) (*kafka.Dialer, error) {
	dialer := &kafka.Dialer{
		Timeout:   10 * time.Second,
		DualStack: true,
	}

	if c.EnableTLS {
		var rootCAPool *x509.CertPool
		if c.TLSRootCACertificate != "" {
			rootCrtFile, err := os.ReadFile(c.TLSRootCACertificate)
			if err != nil {
				return nil, fmt.Errorf("failed to read root CA: %w", err)
			}
			rootCAPool = x509.NewCertPool()
			if !rootCAPool.AppendCertsFromPEM(rootCrtFile) {
				return nil, fmt.Errorf("no certificates found in root CA file %s", c.TLSRootCACertificate)
			}
			c.TLSInsecure = false
		}
		dialer.TLS = &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: c.TLSInsecure,
			RootCAs:            rootCAPool,
		}
	}

	var (
		mechanism sasl.Mechanism
		err       error
	)
	switch strings.ToUpper(c.SASLMechanism) {
	case "":
		return dialer, nil
	case "PLAIN":
		mechanism = plain.Mechanism{Username: c.Username, Password: c.Password}
	case "SCRAM-SHA-256":
		mechanism, err = scram.Mechanism(scram.SHA256, c.Username, c.Password)
	case "SCRAM-SHA-512":
		mechanism, err = scram.Mechanism(scram.SHA512, c.Username, c.Password)
	default:
		return nil, fmt.Errorf("unsupported sasl mechanism %q", c.SASLMechanism)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to set up sasl mechanism %s: %w", c.SASLMechanism, err)
	}
	dialer.SASLMechanism = mechanism
	return dialer, nil
}

type cephfsEvent struct {
	// Mask/Path are the event mask and path of the according entity
	Mask int    `json:"mask"`
	Path string `json:"path"`

	// Src*/Dst* are emitted for the source and destination of move events
	SrcMask  int    `json:"src_mask"`
	SrcPath  string `json:"src_path"`
	DestMask int    `json:"dest_mask"`
	DestPath string `json:"dest_path"`
}

// cephfsScan is a single tree.Scan call derived from an event
type cephfsScan struct {
	path   string
	action watcher.EventAction
	isDir  bool
}

func (w *CephFSWatcher) Watch(topic string) {
	w.log.Info().Str("topic", topic).Msg("cephfs watcher watching topic")
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers: w.brokers,
		GroupID: "opencloud-posixfs",
		Topic:   topic,
		Dialer:  w.dialer,
	})

	for {
		m, err := r.ReadMessage(context.Background())
		if err != nil {
			w.log.Error().Err(err).Msg("error reading message")
			continue
		}

		ev := &cephfsEvent{}
		err = json.Unmarshal(m.Value, ev)
		if err != nil {
			w.log.Error().Err(err).Msg("error unmarshalling message")
			continue
		}

		// The MDS keeps the order of the events it emits, so scan them in
		// the order they arrive. Scan only debounces, except for deletes
		// and moves which have to happen before later events for the path.
		for _, s := range w.scans(ev) {
			if err := w.tree.Scan(s.path, s.action, s.isDir); err != nil {
				w.log.Error().Err(err).Str("path", s.path).Msg("error scanning file")
			}
		}
	}
}

// scans translates an event into the scans to run, in order. The event
// paths are relative to the watch root, the ignorer only knows absolute
// paths, so each path is made absolute before checking it.
func (w *CephFSWatcher) scans(ev *cephfsEvent) []cephfsScan {
	var scans []cephfsScan
	add := func(rel string, action watcher.EventAction, isDir bool) {
		path := filepath.Join(w.root, rel)
		if w.tree.Ignorer.IsIgnored(path) {
			return
		}
		scans = append(scans, cephfsScan{path: path, action: action, isDir: isDir})
	}

	if ev.DestMask > 0 {
		// A move. Either half may be ignored, e.g. when a file is moved
		// into place from the tmp dir or into the trash, so handle them
		// separately like the inotify watcher does.
		if ev.DestMask&CEPH_MDS_NOTIFY_MOVED_TO == 0 {
			w.log.Trace().Interface("event", ev).Msg("unhandled event")
			return nil
		}
		isDir := ev.DestMask&CEPH_MDS_NOTIFY_ONLYDIR > 0
		if ev.SrcMask > 0 {
			add(ev.SrcPath, watcher.ActionMoveFrom, isDir)
		}
		add(ev.DestPath, watcher.ActionMove, isDir)
		return scans
	}

	isDir := ev.Mask&CEPH_MDS_NOTIFY_ONLYDIR > 0
	switch {
	case ev.Mask&CEPH_MDS_NOTIFY_DELETE > 0:
		add(ev.Path, watcher.ActionDelete, isDir)
	case ev.Mask&CEPH_MDS_NOTIFY_CREATE > 0:
		add(ev.Path, watcher.ActionCreate, isDir)
	case ev.Mask&CEPH_MDS_NOTIFY_CLOSE_WRITE > 0:
		add(ev.Path, watcher.ActionUpdate, isDir)
	case ev.Mask&CEPH_MDS_NOTIFY_CLOSE > 0:
		// ignore, already handled by CLOSE_WRITE
	default:
		w.log.Trace().Interface("event", ev).Msg("unhandled event")
	}
	return scans
}
