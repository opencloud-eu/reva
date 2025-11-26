package ocmd

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestOcmd(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Ocmd Suite")
}
