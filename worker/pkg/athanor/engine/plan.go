package engine

// plan.go — première tranche du COMPILATEUR (RFC §5) et du GRAPHE DE DÉPENDANCES
// (RFC §6). Compile transforme une description déclarative (Spec) en plan
// exécutable et validé : colonnes existantes, pas de conflit d'écriture, et
// RowTransformers ordonnés par leurs dépendances de colonnes (tri topologique
// avec détection de cycle). Un plan qui compile s'exécute.

import (
	"fmt"
	"strings"

	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/transform"
)

// ValueBinding lie une colonne à un transformer de portée Value.
type ValueBinding struct {
	Column string
	T      transform.ValueTransformer
}

// Spec est la description déclarative, avant compilation.
type Spec struct {
	Values []ValueBinding             // transformers colonne par colonne
	Rows   []transform.RowTransformer // transformers multi-colonnes
}

// Plan est la forme compilée et validée d'une Spec, prête à exécuter.
type Plan struct {
	values   []ValueBinding
	rows     []transform.RowTransformer // dans l'ordre topologique
	rowOrder []int                      // indices d'origine, pour l'explicabilité
}

// Compile valide la Spec contre un schéma (l'ensemble des colonnes disponibles)
// et produit un Plan. Erreurs possibles : colonne inconnue, conflit d'écriture,
// cycle de dépendances entre RowTransformers.
func Compile(schema []string, spec Spec) (*Plan, error) {
	known := make(map[string]bool, len(schema))
	for _, c := range schema {
		known[c] = true
	}

	// writers[col] = description de qui écrit la colonne (pour détecter les conflits).
	writers := map[string]string{}

	for _, vb := range spec.Values {
		if !known[vb.Column] {
			return nil, fmt.Errorf("compile: colonne inconnue dans un binding valeur: %q", vb.Column)
		}
		if prev, ok := writers[vb.Column]; ok {
			return nil, fmt.Errorf("compile: conflit d'écriture sur %q (%s et binding valeur)", vb.Column, prev)
		}
		writers[vb.Column] = "binding valeur"
	}

	for i, rt := range spec.Rows {
		for _, c := range rt.Reads() {
			if !known[c] {
				return nil, fmt.Errorf("compile: transformer ligne #%d lit une colonne inconnue: %q", i, c)
			}
		}
		for _, c := range rt.Writes() {
			if !known[c] {
				return nil, fmt.Errorf("compile: transformer ligne #%d écrit une colonne inconnue: %q", i, c)
			}
			if prev, ok := writers[c]; ok {
				return nil, fmt.Errorf("compile: conflit d'écriture sur %q (%s et transformer ligne #%d)", c, prev, i)
			}
			writers[c] = fmt.Sprintf("transformer ligne #%d", i)
		}
	}

	order, err := topoSortRows(spec.Rows)
	if err != nil {
		return nil, err
	}

	ordered := make([]transform.RowTransformer, len(order))
	for pos, idx := range order {
		ordered[pos] = spec.Rows[idx]
	}
	return &Plan{values: spec.Values, rows: ordered, rowOrder: order}, nil
}

// topoSortRows ordonne les RowTransformers pour qu'un transformer qui ÉCRIT une
// colonne s'exécute avant ceux qui la LISENT. Renvoie les indices d'origine dans
// l'ordre d'exécution, ou une erreur si un cycle est détecté.
func topoSortRows(rows []transform.RowTransformer) ([]int, error) {
	n := len(rows)
	// Arête i -> j si une écriture de i est lue par j (i doit précéder j).
	adj := make([][]bool, n)
	indeg := make([]int, n)
	for i := range adj {
		adj[i] = make([]bool, n)
	}
	for i := 0; i < n; i++ {
		writesI := toSet(rows[i].Writes())
		for j := 0; j < n; j++ {
			if i == j {
				continue
			}
			if intersects(writesI, rows[j].Reads()) && !adj[i][j] {
				adj[i][j] = true
				indeg[j]++
			}
		}
	}

	// Kahn : on retire itérativement les nœuds sans dépendance entrante.
	queue := []int{}
	for i := 0; i < n; i++ {
		if indeg[i] == 0 {
			queue = append(queue, i)
		}
	}
	order := make([]int, 0, n)
	for len(queue) > 0 {
		v := queue[0]
		queue = queue[1:]
		order = append(order, v)
		for j := 0; j < n; j++ {
			if adj[v][j] {
				indeg[j]--
				if indeg[j] == 0 {
					queue = append(queue, j)
				}
			}
		}
	}
	if len(order) != n {
		return nil, fmt.Errorf("compile: cycle de dépendances entre transformers ligne (colonnes lues/écrites circulaires)")
	}
	return order, nil
}

func toSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

func intersects(set map[string]bool, list []string) bool {
	for _, s := range list {
		if set[s] {
			return true
		}
	}
	return false
}

// Explain renvoie une description lisible du plan (base de l'« explain » du preview).
func (p *Plan) Explain() string {
	var b strings.Builder
	b.WriteString("Plan compilé:\n")
	for _, vb := range p.values {
		fmt.Fprintf(&b, "  value  %-16s -> %T\n", vb.Column, vb.T)
	}
	for pos, rt := range p.rows {
		fmt.Fprintf(&b, "  row #%d %T  lit=%v écrit=%v\n", pos, rt, rt.Reads(), rt.Writes())
	}
	return b.String()
}
