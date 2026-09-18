package assimilation

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAssimilation(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Assimilation Suite")
}
