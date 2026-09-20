-- Base CIBLE : même structure, vide. Le moteur y écrit les données anonymisées.
CREATE TABLE clients (
    id     INT PRIMARY KEY,
    prenom VARCHAR(100),
    nom    VARCHAR(100),
    email  VARCHAR(255)
);

CREATE TABLE commandes (
    id        INT PRIMARY KEY,
    reference VARCHAR(40)
);

CREATE TABLE lignes (
    id          INT PRIMARY KEY,
    commande_id INT NOT NULL,
    libelle     VARCHAR(100),
    CONSTRAINT fk_lignes_commande FOREIGN KEY (commande_id) REFERENCES commandes (id)
);
