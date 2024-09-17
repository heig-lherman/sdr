Solution 2
==========

> Que se passe-t-il dans RR version "exactement une fois" si le traitement est très long ?

En cas de traitement long:

- le client timeout et renvoie la requête.
- le serveur utilise l'id pour détecter le doublon et l'ignore
