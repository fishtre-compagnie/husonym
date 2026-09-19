-- Base CIBLE : même structure, vide. Le moteur y écrit les données anonymisées.
CREATE TABLE clients (
    id     INT PRIMARY KEY,
    prenom VARCHAR(100),
    nom    VARCHAR(100),
    email  VARCHAR(255)
);
