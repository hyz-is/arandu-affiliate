package unit_test

import (
	"testing"

	"github.com/arandu-io/kyse/components"

	affiliate "github.com/hyz-is/arandu-affiliate"
)

// TestTheTableStateKeepsWhatTheRequestAsked reads a list's state the way a data
// table does: the address of the next step carries the search, the filters, the
// order and the columns switched off, so a step never loses one of them.
func TestTheTableStateKeepsWhatTheRequestAsked(t *testing.T) {
	t.Parallel()

	state := affiliate.TableState{
		URL: "/afiliado/admin/commissions", Query: "ana", Sort: "amount", Dir: "desc", CurrentPage: 2, Pages: 4, Total: 90,
		Rows: 25, PerPage: 25, Facets: map[string][]string{"level": {"1"}, "status": {"paid"}}, Hidden: []string{"order"},
	}.State()

	got := state.Values(components.DataTableChange{Page: 3})
	if got.Get("q") != "ana" || got.Get("sort") != "amount" || got.Get("dir") != "desc" || got.Get("page") != "3" {
		t.Errorf("the next page lost the search or the order: %v", got)
	}
	if got.Get("level") != "1" || got.Get("status") != "paid" {
		t.Errorf("the next page lost a filter: %v", got)
	}
	if hide := got["hide"]; len(hide) != 1 || hide[0] != "order" {
		t.Errorf("the next page lost the hidden column: %v", got)
	}
}

// TestTheTablePagerKnowsWhereTheListStands is the footer's source: the page
// being shown and how many rows the whole list holds.
func TestTheTablePagerKnowsWhereTheListStands(t *testing.T) {
	t.Parallel()

	pager := affiliate.TableState{URL: "/afiliado/me", CurrentPage: 2, Pages: 4, Total: 90, Rows: 25, PerPage: 25}.Pager()
	if !pager.HasPages() || pager.OnFirstPage() || !pager.HasMorePages() {
		t.Errorf("page 2 of 4 reads as first %v, last %v, several %v", pager.OnFirstPage(), !pager.HasMorePages(), pager.HasPages())
	}
}
