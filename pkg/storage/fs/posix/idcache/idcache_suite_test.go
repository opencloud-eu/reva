package idcache_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestIdcache(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Idcache Suite")
}
