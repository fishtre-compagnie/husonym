# tsqlparser (généré)

Parseur T-SQL basé sur ANTLR v4, utilisé pour qualifier les colonnes des clauses
`WHERE` lors du subsetting SQL Server (voir
`worker/pkg/select-query-builder/tsql`).

Ce package remplace la dépendance externe `github.com/nucleuscloud/go-antlrv4-parser`
(dépôt tiers désormais archivé et **sans licence**). Le parseur est ici **régénéré
nous-mêmes** depuis la grammaire T-SQL publique, ce qui donne un artefact sous une
lignée de licence claire et intégré au monorepo.

## Provenance

- **Grammaire source** : `grammar/TSqlLexer.g4` + `grammar/TSqlParser.g4`, copiées
  depuis [`antlr/grammars-v4`](https://github.com/antlr/grammars-v4/tree/master/sql/tsql).
- **Licence de la grammaire** : MIT (voir l'en-tête de `grammar/TSqlParser.g4`).
- **Outil** : ANTLR 4.13.2, cible Go (runtime `github.com/antlr4-go/antlr/v4`).

## Régénérer

Les fichiers `tsql_lexer.go`, `tsql_parser.go`, `tsqlparser_listener.go` et
`tsqlparser_base_listener.go` sont **générés — ne pas les éditer à la main**.

```bash
./internal/tsqlparser/gen.sh
```

Le script utilise Docker (aucune installation de Java requise). Pour mettre à jour
la grammaire, remplacer les fichiers de `grammar/` puis relancer `gen.sh`.
