package schema

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_MySQLRenderer(t *testing.T) {
	table := &Table{
		Name: "COMMANDE",
		Columns: []Column{
			{Name: "id", Type: Uint64(), AutoIncrement: true},
			{Name: "order", Type: Varchar(20), Nullable: true, Collation: "utf8mb4_bin"},
			{Name: "parent_id", Type: Int64(), Default: "0"},
			{Name: "etat", Type: Native(map[Dialect]string{MySQL: "ENUM('a','')"})},
		},
		PrimaryKey: []string{"id"},
		Indexes:    []Index{{Name: "uq_order", Columns: []string{"order"}, Unique: true}},
		ForeignKeys: []ForeignKey{
			{Name: "fk_parent", Columns: []string{"parent_id"}, RefTable: "COMMANDE", RefColumns: []string{"id"}, OnDelete: "CASCADE"},
			{Name: "fk_virtual", Columns: []string{"parent_id"}, RefTable: "COMMANDE", RefColumns: []string{"id"}, Virtual: true},
		},
	}
	r := MySQLRenderer{}

	create, err := r.CreateTable("bench_x", table)
	require.NoError(t, err)
	require.Equal(t, "CREATE TABLE `bench_x`.`COMMANDE` (\n"+
		"  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,\n"+
		"  `order` VARCHAR(20) COLLATE utf8mb4_bin,\n"+
		"  `parent_id` BIGINT NOT NULL DEFAULT 0,\n"+
		"  `etat` ENUM('a','') NOT NULL,\n"+
		"  PRIMARY KEY (`id`),\n"+
		"  UNIQUE KEY `uq_order` (`order`)\n"+
		") ENGINE=InnoDB", create)

	require.Equal(t, []string{
		"ALTER TABLE `bench_x`.`COMMANDE` ADD CONSTRAINT `fk_parent` FOREIGN KEY (`parent_id`) " +
			"REFERENCES `bench_x`.`COMMANDE` (`id`) ON DELETE CASCADE",
	}, r.AddForeignKeys("bench_x", table), "virtual foreign keys are not declared")

	_, err = r.CreateTable("bench_x", &Table{Name: "T", Columns: []Column{{Name: "c", Type: Native(nil)}}})
	require.Error(t, err)
}
