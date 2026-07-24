package transform

// example_row.go — démonstration concrète de la capacité NOUVELLE débloquée par
// le Mouvement 1 : un transformer de portée ROW, impossible dans le modèle
// Benthos actuel où un transformer ne voit qu'une seule valeur isolée.
//
// C'est l'exemple « prénom selon le sexe » de la RFC (§5) : la règle lit une
// colonne (sexe) pour décider comment en écrire une autre (prénom).

// FirstNameBySex écrit un prénom cohérent avec la valeur d'une colonne de sexe.
// Les noms sont ici des valeurs de démonstration ; en cible ils seront produits
// par faker.FirstName(gender) branché sur la cohérence déterministe (RFC §8).
type FirstNameBySex struct {
	SexColumn  string // colonne lue (ex. "sexe")
	NameColumn string // colonne écrite (ex. "prenom")
}

func (t FirstNameBySex) Reads() []string  { return []string{t.SexColumn} }
func (t FirstNameBySex) Writes() []string { return []string{t.NameColumn} }

func (t FirstNameBySex) TransformRow(_ Ctx, row Row) error {
	switch row.Str(t.SexColumn) {
	case "H", "M", "Homme":
		return row.Set(t.NameColumn, "Karim") // TODO(Mouvement 2) : faker.FirstName(male) déterministe
	case "F", "Femme":
		return row.Set(t.NameColumn, "Léa") // TODO(Mouvement 2) : faker.FirstName(female) déterministe
	default:
		return row.Set(t.NameColumn, "Alex")
	}
}

// Vérification à la compilation : c'est bien un transformer de portée Row.
var _ RowTransformer = FirstNameBySex{}
