package tree

// Assimilate exposes assimilate to the tests of the tree_test package
func (t *Tree) Assimilate(path string) error {
	return t.assimilate(scanItem{Path: path})
}
