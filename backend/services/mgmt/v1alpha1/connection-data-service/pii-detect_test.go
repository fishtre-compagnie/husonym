package v1alpha1_connectiondataservice

import "testing"

// isAnalyzableText décide si une colonne part vers Presidio. Un faux « non »
// rendrait la colonne invisible au scan de contenu ; un faux « oui » coûte 20
// appels HTTP. Les deux cas sont donc couverts explicitement.
func Test_isAnalyzableText(t *testing.T) {
	cases := []struct {
		name   string
		values []string
		want   bool
	}{
		{
			name:   "colonne vide",
			values: []string{"", "   "},
			want:   false,
		},
		{
			name:   "identifiants entiers",
			values: []string{"1", "2", "347", "1204"},
			want:   false,
		},
		{
			name:   "montants décimaux",
			values: []string{"1204.50", "89,90", "-15.00"},
			want:   false,
		},
		{
			name:   "booléens",
			values: []string{"true", "false", "1", "0", "oui", "non"},
			want:   false,
		},
		{
			name: "références sans lettre",
			// Le NER raisonne sur des mots : une suite de chiffres et de
			// séparateurs ne peut porter ni nom ni lieu.
			values: []string{"12-34-56", "78/90/12", "00.11.22"},
			want:   false,
		},
		{
			name:   "noms de personnes",
			values: []string{"Camille Durand", "Jean Petit", "Awa Diallo"},
			want:   true,
		},
		{
			name:   "villes",
			values: []string{"Bordeaux", "Lille", "Saint-Étienne"},
			want:   true,
		},
		{
			name:   "adresses mêlant chiffres et mots",
			values: []string{"12 rue des Lilas", "3 bis avenue Foch", "8 place Gambetta"},
			want:   true,
		},
		{
			name: "colonne numérique avec une valeur textuelle isolée",
			// Majorité de nombres : la valeur texte est plus probablement une
			// anomalie de saisie qu'une donnée personnelle. Ne pas la faire
			// basculer évite de payer le NER sur toute la colonne.
			values: []string{"10", "20", "30", "40", "N/A"},
			want:   false,
		},
		{
			name: "colonne textuelle avec quelques valeurs numériques",
			// Inversement, une majorité de texte doit partir au NER même si
			// certaines lignes contiennent un code.
			values: []string{"Camille", "Jean", "Awa", "0", "42"},
			want:   true,
		},
		{
			name:   "les valeurs vides ne comptent pas dans le ratio",
			values: []string{"Camille Durand", "", "   ", ""},
			want:   true,
		},
		{
			name: "signatures base64 (cas relevé en production)",
			// Colonne LB_SIGNATURE d'une base réelle : zlib compressé puis base64.
			// Presidio y voyait des noms de villes.
			values: []string{
				"eJztWPdXk1m0jcMINmTUEUYQsCAovfdiG6QFHIooVSGEDtJbQnBUGKSNCS2hRASCNJFOqCoCBogM",
				"eJztevk31Hv8/0ShBWUtsrSqZMkaskRFsmWdDKbIMsYWoghJSLKUxIxlrGOn+VgmNEiWjxi7wTCW",
				"eJzteokr3O3/9EipZOluIXt3ualsZd+nRSoZSrLviSxjZzAYKtyyTBJuY8+WbNkZyyiEZB0MBmPJ",
			},
			want: false,
		},
		{
			name: "base64 replié en lignes de 76 caractères",
			// Forme réelle en base : le saut de ligne faisait échouer un test
			// portant sur la valeur entière.
			values: []string{
				"eJztWPdXk1m0jcMINmTUEUYQsCAovfdiG6QFHIooVSGEDtJbQnBUGKSNCS2hRASCNJFOqCoCBogM\neJztevk31Hv8/0ShBWUtsrSqZMkaskRFsmWdDKbIMsYWoghJSLKUxIxlrGOn+VgmNEiWjxi7wTCW",
				"eJzteokr3O3/9EipZOluIXt3ualsZd+nRSoZSrLviSxjZzAYKtyyTBJuY8+WbNkZyyiEZB0MBmPJ\neJztu2VU1E/cNg6ioEgoKiAt0i2NpNKxgIh0d6d0i7SAtCzSAlLSSywpzRLiSi0pKS1L9zPr737e",
			},
			want: false,
		},
		{
			name: "texte libre long sans ponctuation",
			// Blancs retirés, cette phrase forme une suite conforme à l'alphabet
			// base64 : c'est précisément le cas que le découpage par mot protège.
			// Une régression ici rendrait le texte libre invisible au NER.
			values: []string{
				"Client Jean Dupont joignable au 0710203040",
				"Rappeler Marie Martin avant midi svp",
				"Contact sur place Awa Diallo batiment B",
			},
			want: true,
		},
		{
			name: "base64 replié dont la dernière ligne est courte",
			// Cas qui faisait échouer un critère « chaque fragment est long » :
			// le dernier bloc d'un base64 ne fait que quelques caractères.
			values: []string{
				"eJztWPdXk1m0jcMINmTUEUYQsCAovfdiG6QFHIooVSGEDtJbQnBUGKSNCS2hRASCNJFOqCoCBogM\neJztevk31Hv8/0ShBWUtsrSqZMkaskRFsmWdDKbIMsYWoghJSLKUxIxlrGOn+VgmNEiWjxi7wTCW\nAbc=",
				"eJzteokr3O3/9EipZOluIXt3ualsZd+nRSoZSrLviSxjZzAYKtyyTBJuY8+WbNkZyyiEZB0MBmPJ\neJztu2VU1E/cNg6ioEgoKiAt0i2NpNKxgIh0d6d0i7SAtCzSAlLSSywpzRLiSi0pKS1L9zPr737e\nQg==",
			},
			want: false,
		},
		{
			name:   "jetons et hashs",
			values: []string{"d41d8cd98f00b204e9800998ecf8427e", "a3f5b8c9d0e1f2a3b4c5d6e7f8a9b0c1"},
			want:   false,
		},
		{
			name: "un email long n'est pas un blob",
			// L'arobase et le point sont hors de l'alphabet base64 : le filtre
			// opaque ne doit pas les emporter, même sur une adresse très longue.
			values: []string{
				"prenom.nom.tres.long@sous-domaine.exemple-entreprise.fr",
				"autre.personne.contact@sous-domaine.exemple-entreprise.fr",
			},
			want: true,
		},
		{
			name:   "mots composés plus courts que le seuil de blob",
			values: []string{"Saint-Étienne-du-Rouvray", "Villeneuve-d'Ascq", "Jean-Pierre"},
			want:   true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isAnalyzableText(tc.values); got != tc.want {
				t.Errorf("isAnalyzableText(%q) = %v, attendu %v", tc.values, got, tc.want)
			}
		})
	}
}
