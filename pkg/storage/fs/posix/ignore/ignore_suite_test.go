package ignore_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestIgnore(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Ignore Suite")
}
