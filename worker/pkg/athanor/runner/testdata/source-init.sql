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
