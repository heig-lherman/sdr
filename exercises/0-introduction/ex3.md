En cas de panne du client:

- Lorsqu'il revient, s'il veut envoyer un nouveau message (potentiellement indépendant du précédent), celui-ci sera ignoré par le serveur
- Le client réessaiera infiniment, entrant dans une boucle infinie

Solution:

Le client doit envoyer un identifiant unique de l'écécution du cours.

Ainsi, les ids seront différents lors de la seconde éxécution, et le serveur ne croira pas à un doublon.

Une manière de garantir cette unicité des identifiants peut être d'y accoler l'heure du début de son éxécution.
