package trinkets

// A sort of more than one level: by department, and within a department by
// salary, highest first.
//
// Each level settles only what the levels above it left equal, so the answer
// does not depend on what the rows were sorted by before -- which is the whole
// difference between a run of levels and sorting twice.

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// staffTree is four people across two departments, with a salary column that
// sorts numerically and a start-date column that does not.
func staffTree(t *testing.T) *TreeView {
	t.Helper()
	tv := NewTreeView()

	dept := NewTreeColumn("dept", "Department", 12*cell)
	dept.Sortable = true
	tv.AddColumn(dept)

	salary := NewTreeColumn("salary", "Salary", 10*cell)
	salary.Sortable = true
	salary.Numeric = true
	tv.AddColumn(salary)

	for _, spec := range []struct{ name, dept, salary string }{
		{"ash", "sales", "40,000"},
		{"bo", "engineering", "90,000"},
		{"cy", "sales", "120,000"},
		{"dee", "engineering", "90,000"},
	} {
		it := NewTreeItem(spec.name)
		it.SetValue("dept", spec.dept)
		it.SetValue("salary", spec.salary)
		tv.AddRootItem(it)
	}
	tv.SetBounds(core.UnitRect{Width: 640, Height: 200})
	return tv
}

// The second level decides only where the first found no difference.
func TestASecondLevelSettlesWhatTheFirstLeftEqual(t *testing.T) {
	tv := staffTree(t)

	tv.SetSortLevels(SortLevel{By: 0}, SortLevel{By: 1, Descending: true})
	want := []string{"bo", "dee", "cy", "ash"}
	if got := visualCaptions(tv); !equalStrings(got, want) {
		t.Errorf("by department, then salary descending = %v, want %v", got, want)
	}

	// Turning the second level round moves only the rows the first tied.
	tv.SetSortLevels(SortLevel{By: 0}, SortLevel{By: 1})
	want = []string{"bo", "dee", "ash", "cy"}
	if got := visualCaptions(tv); !equalStrings(got, want) {
		t.Errorf("by department, then salary ascending = %v, want %v", got, want)
	}
}

// Rows equal on every level keep the order the app gave them.
func TestRowsEqualAllTheWayDownKeepTheirOrder(t *testing.T) {
	tv := staffTree(t)

	// bo and dee are the same department at the same salary, and bo was
	// added first.
	tv.SetSortLevels(SortLevel{By: 0}, SortLevel{By: 1})
	if got := visualCaptions(tv); got[0] != "bo" || got[1] != "dee" {
		t.Errorf("a full tie reordered the rows: %v", got)
	}
}

// A level on its own is the sort that was there before, so one level and the
// single-level call mean the same thing.
func TestOneLevelIsTheSortThatWasAlwaysThere(t *testing.T) {
	tv := staffTree(t)

	tv.SetSorted(true, 1, true)
	byCall := visualCaptions(tv)

	tv.SetSortLevels(SortLevel{By: 1, Descending: true})
	byLevels := visualCaptions(tv)

	if !equalStrings(byCall, byLevels) {
		t.Errorf("SetSorted gave %v and one level gave %v", byCall, byLevels)
	}
	if sorted, by, desc := tv.Sorted(); !sorted || by != 1 || !desc {
		t.Errorf("Sorted() = %v, %d, %v after one descending level", sorted, by, desc)
	}
}

// Adding a level puts it beneath the ones already there. A column already in
// the run keeps its place and takes the new direction, one column not being
// able to be two levels of one sort.
func TestAddingALevelPutsItUnderneath(t *testing.T) {
	tv := staffTree(t)

	tv.SetSorted(true, 0, false)
	tv.AddSortLevel(1, true)
	if got, want := len(tv.SortLevels()), 2; got != want {
		t.Fatalf("the run holds %d levels, want %d: %#v", got, want, tv.SortLevels())
	}
	want := []string{"bo", "dee", "cy", "ash"}
	if got := visualCaptions(tv); !equalStrings(got, want) {
		t.Errorf("added level = %v, want %v", got, want)
	}

	tv.AddSortLevel(0, true)
	levels := tv.SortLevels()
	if len(levels) != 2 {
		t.Fatalf("a column already in the run was added twice: %#v", levels)
	}
	if levels[0].By != 0 || !levels[0].Descending {
		t.Errorf("the column kept %#v, want its place and the new direction", levels[0])
	}
	want = []string{"cy", "ash", "bo", "dee"}
	if got := visualCaptions(tv); !equalStrings(got, want) {
		t.Errorf("first level turned round = %v, want %v", got, want)
	}
}

// The header indicator and the header cycle read the first level, and the
// levels beneath it are left where they are.
func TestTheHeaderTurnsTheFirstLevelOnly(t *testing.T) {
	tv := staffTree(t)
	tv.SetSortLevels(SortLevel{By: 0}, SortLevel{By: 1, Descending: true})

	tv.headerSortClick(tv.columns[0]) // second activation on the first level
	levels := tv.SortLevels()
	if len(levels) != 1 {
		t.Fatalf("a header activation left %d levels: %#v", len(levels), levels)
	}
	if !levels[0].Descending {
		t.Error("the header activation did not reverse the first level")
	}
	if !tv.sortIndicatorFor(tv.columns[0]) {
		t.Error("the indicator left the column the first level is on")
	}
	if tv.sortIndicatorFor(tv.columns[1]) {
		t.Error("the indicator sits on a column the first level is not on")
	}
}

// Handing over no levels at all turns the sort off, and the rows read in the
// order the app gave them.
func TestNoLevelsIsNoSort(t *testing.T) {
	tv := staffTree(t)
	tv.SetSortLevels(SortLevel{By: 0}, SortLevel{By: 1})

	tv.SetSortLevels()
	if sorted, _, _ := tv.Sorted(); sorted {
		t.Error("the view still reports itself sorted")
	}
	want := []string{"ash", "bo", "cy", "dee"}
	if got := visualCaptions(tv); !equalStrings(got, want) {
		t.Errorf("unsorted = %v, want the order the app gave: %v", got, want)
	}
}
