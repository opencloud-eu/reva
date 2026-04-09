package userprovider_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestUserprovider(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Userprovider Suite")
}
