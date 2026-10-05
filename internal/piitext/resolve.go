package piitext

import (
	"cmp"
	"slices"
	"strings"
)

// resolve turns the targets of a value, which may overlap, into targets that do not, in the
// order of the text. Every byte a target covers is covered by one target of the result, so
// nothing an analyzer found is left as it was.
//
// The rules are those of Presidio's anonymizer, made independent of the order of the findings:
//
//  1. Targets of one entity type that overlap become one, over all they cover.
//  2. A target inside another is dropped. Of two targets on the same bytes, the higher score
//     stays, and with one score the entity type that sorts first.
//  3. Where targets of two entity types overlap, each byte belongs to the one of highest rank:
//     the higher score, then the one that starts first, then the entity type that sorts first.
//     A target keeps the bytes that belong to it, and is dropped when none does.
//  4. Two neighbors of one entity type that only spaces separate become one, spaces included.
func resolve(value string, targets []target) []target {
	if len(targets) == 0 {
		return nil
	}
	resolved := dropContained(mergeSameEntity(targets))
	resolved = splitOverlaps(resolved)
	return mergeOverSpaces(value, resolved)
}

// byPosition orders targets by where they start, then where they end, then by entity type.
func byPosition(a, b target) int {
	return cmp.Or(cmp.Compare(a.start, b.start), cmp.Compare(a.end, b.end), cmp.Compare(a.entity, b.entity))
}

// mergeSameEntity is rule 1. Targets that only touch do not overlap, and stay apart.
func mergeSameEntity(targets []target) []target {
	sorted := slices.Clone(targets)
	slices.SortFunc(sorted, func(a, b target) int {
		return cmp.Or(cmp.Compare(a.entity, b.entity), byPosition(a, b))
	})
	merged := sorted[:0]
	for _, next := range sorted {
		if last := len(merged) - 1; last >= 0 && merged[last].entity == next.entity && next.start < merged[last].end {
			merged[last].end = max(merged[last].end, next.end)
			merged[last].score = max(merged[last].score, next.score)
			continue
		}
		merged = append(merged, next)
	}
	return merged
}

// dropContained is rule 2.
func dropContained(targets []target) []target {
	kept := make([]target, 0, len(targets))
	for i, inner := range targets {
		contained := false
		for j, outer := range targets {
			if i == j || outer.start > inner.start || outer.end < inner.end {
				continue
			}
			if outer.span != inner.span || outranksOnSameSpan(outer, inner) {
				contained = true
				break
			}
		}
		if !contained {
			kept = append(kept, inner)
		}
	}
	return kept
}

// outranksOnSameSpan tells which of two targets on the same bytes stays. After rule 1 they are
// of two entity types, so one of them always does.
func outranksOnSameSpan(a, b target) bool {
	if a.score != b.score {
		return a.score > b.score
	}
	return a.entity < b.entity
}

// outranks is the rank of rule 3.
func outranks(a, b target) bool {
	if a.score != b.score {
		return a.score > b.score
	}
	if a.start != b.start {
		return a.start < b.start
	}
	return a.entity < b.entity
}

// splitOverlaps is rule 3. No target contains another here, so the bytes that belong to a
// target are one run: a target of higher rank can only take its beginning or its end. The
// result is in the order of the text.
func splitOverlaps(targets []target) []target {
	sorted := slices.Clone(targets)
	slices.SortFunc(sorted, byPosition)

	// The value is cut at every start and every end: between two cuts, the same targets cover
	// every byte, and the one of highest rank owns them all.
	cuts := make([]int, 0, 2*len(sorted))
	for _, target := range sorted {
		cuts = append(cuts, target.start, target.end)
	}
	slices.Sort(cuts)
	cuts = slices.Compact(cuts)

	owned := make([]span, len(sorted))
	for i := range owned {
		owned[i] = span{start: -1}
	}
	for i := 0; i+1 < len(cuts); i++ {
		from, to := cuts[i], cuts[i+1]
		owner := -1
		for j, target := range sorted {
			if target.start <= from && to <= target.end && (owner < 0 || outranks(target, sorted[owner])) {
				owner = j
			}
		}
		if owner < 0 {
			continue
		}
		if owned[owner].start < 0 {
			owned[owner].start = from
		}
		owned[owner].end = to
	}

	split := make([]target, 0, len(sorted))
	for i, target := range sorted {
		if owned[i].start < 0 {
			continue
		}
		target.span = owned[i]
		split = append(split, target)
	}
	slices.SortFunc(split, byPosition)
	return split
}

// mergeOverSpaces is rule 4, on targets that do not overlap and follow the text. A tab, a line
// break or a no-break space is not a space.
func mergeOverSpaces(value string, targets []target) []target {
	merged := targets[:0]
	for _, next := range targets {
		if last := len(merged) - 1; last >= 0 && merged[last].entity == next.entity {
			between := value[merged[last].end:next.start]
			if between != "" && strings.Trim(between, " ") == "" {
				merged[last].end = next.end
				merged[last].score = max(merged[last].score, next.score)
				continue
			}
		}
		merged = append(merged, next)
	}
	return merged
}
