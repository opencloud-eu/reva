// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package revisions

type SimplePlan struct {
	retained    []Revision
	deleted     []Revision
	safeguarded []Revision
}

var _ Plan = &SimplePlan{}

func NewSimplePlan() Plan {
	return &SimplePlan{
		retained:    []Revision{},
		deleted:     []Revision{},
		safeguarded: []Revision{},
	}
}

func (p *SimplePlan) Delete(rev ...Revision) {
	p.deleted = append(p.deleted, rev...)
}

func (p *SimplePlan) Retain(rev ...Revision) {
	p.retained = append(p.retained, rev...)
}

func (p *SimplePlan) Safeguard(rev ...Revision) {
	p.safeguarded = append(p.safeguarded, rev...)
}

func (p *SimplePlan) Deleted() []Revision {
	return p.deleted
}

func (p *SimplePlan) Retained() []Revision {
	return p.retained
}

func (p *SimplePlan) Safeguarded() []Revision {
	return p.safeguarded
}
