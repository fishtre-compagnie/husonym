-- Base SOURCE : une table clients avec des données réalistes à anonymiser.
CREATE TABLE clients (
    id     INT PRIMARY KEY,
    prenom VARCHAR(100),
    nom    VARCHAR(100),
    email  VARCHAR(255)
);

INSERT INTO clients (id, prenom, nom, email) VALUES
    (1, 'Jean',   'Dupont',  'jean.dupont@gmail.com'),
    (2, 'Marie',  'Martin',  'marie.martin@yahoo.fr'),
    (3, 'Pierre', 'Bernard', 'pierre.bernard@orange.fr'),
    (4, 'Sophie', 'Petit',   'sophie.petit@free.fr'),
    (5, 'Luc',    'Durand',  'luc.durand@hotmail.com');

-- Commandes et leurs lignes : une clé étrangère obligatoire vers une table que le job ne
-- copie qu'en partie. La commande 3 tient le rôle de celle qui n'a pas été copiée —
-- laissée hors du subset, ou créée après la lecture de sa table sur une source vivante.
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

INSERT INTO commandes (id, reference) VALUES
    (1, 'CMD-1'), (2, 'CMD-2'), (3, 'CMD-3');

INSERT INTO lignes (id, commande_id, libelle) VALUES
    (10, 1, 'ligne de la commande 1'),
    (11, 2, 'ligne de la commande 2'),
    (12, 3, 'ligne de la commande non copiee'),
    (13, 1, 'autre ligne de la commande 1'),
    (14, 3, 'autre ligne de la commande non copiee');
