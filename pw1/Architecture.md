# Document d'Architecture Logicielle

## Structure générale

Nous allons reprendre la stucture fournie dans le projet de base, en y ajoutant les implémentations nécessaires pour
prendre en charge les garanties demandées. Nos modifications comprendront l'ajout du protocole de transport TCP pour
remplacer UDP et garantir l'ordre d'arrivée ainsi que la détection de perte de paquets.

## Couches logicielles

### Réseau (`transport`)

En partant de l'abstraction `NetworkInterface`, nous offrons une nouvelle implémentation, `TCP`, largement inspirée de
l'implémentation `UDP` fournie. Cette nouvelle implémentation sera conceptualisée comme présenté ci-après.

Le protocole TCP que nous implémenteront devra en outre garantir que les messages envoyés et reçus le soient exactement
une fois, en ajoutant donc une gestion d'identification des messages correct au sens des pannes.

#### État interne

Similairement à `UDP`, `TCP` maintiendra un état interne pour chaque connexion. Cet état sera composé des éléments
suivants :

- Le listener TCP qui accepte les connexions entrantes et l'ID de réception courant associé
- La liste des voisins connus et leur état (connexion, ID d'envoi courant) associé
- La liste des `MessageHandler` souscrits aux messages reçus

#### Goroutines principales

La couche `TCP` doit traiter les processus ainsi que leurs modifications d'états potentielles suivants :

- **Ajout / retrait de handlers** : ajouter ou retirer des `MessageHandler`, modifie la liste des `MessageHandler`
- **Envoi de messages** : envoyer des messages à un voisin, va potentiellement ajouter la connexion à la liste des voisins et mettre à jour l'ID d'envoi courant
- **Réception de messages** : accepter les connexions entrantes, accède à la liste des `MessageHandler`
- **Clôture d'une connexion** : fermer une connexion, retire la connexion de la liste des voisins et la connexion de l'écoute des messages.

Nous utiliserons donc une goroutine principale pour gérer l'état de la connexion TCP que nous nommerons `handleState`
pour rester consistant avec l'implémentation `UDP`. Cela permettra d'avoir un gestionnaire unique pour l'état interne
qui traitera les événements de manière séquentielle, via un stockage dans des variables locales à la goroutines pour
éviter des modifications externes indésirables.

L'écoute des messages reçus se fera dans une goroutine séparée, `listenIncomingMessages`, qui sera lancée lors de 
l'instantiation de l'interface réseau. Cette goroutine s'assurera de désérialiser les messages reçus et de les 
transmettre à la goroutine `handleState` pour le traitement de la vérification de l'ID de message et de la distribution
aux `MessageHandler` souscrits. Une fois le message traité, `handleState` créera un accusé de réception pour le message
reçu et transmettra la demande à la goroutine de la connexion du voisin pour l'envoi dudit accusé.

Cette dernière goroutine, `handleSends`, est responsable de stocker la connexion du voisin et d'envoyer les messages
reçus depuis un channel dedié. Elle sera lancée lorsqu'un message devra être envoyé à ce voisin et gardée en vie tant
que la connexion est active. Ce goroutine enverra les messages de broadcast dans le cadre d'un message de l'utilisateur,
et enverra également l'accusé de réception pour les messages reçus.

Pour être résilient aux pannes, la goroutine `handleSends` devra également gérer les timeouts pour les messages envoyés
et les renvoyer si un accusé de réception n'est pas reçu dans un délai raisonnable. Pour ce faire, nous utiliserons le
mécanisme de `time.After` pour déclencher un renvoi après un certain délai. Comme nous avons une goroutine dédiée pour
chaque serveur voisin, nous pouvons nous permettre de bloquer cette goroutine pour attendre l'accusé de réception.
La goroutine `handeState` transmettra via un channel dédié l'accusé de réception correctement reçu par la goroutine
d'écoute et ainsi permettra à la goroutine `handleSends` d'arrêter le traitement de l'envoi et considérer l'éventuel
message suivant.

Pour résumer le tout, il existe donc trois goroutines principales :

- `handleState` : gestion de l'état interne de la couche TCP, stockage de l'ID courant d'envoi et de réception par voisin
- `listenIncomingMessages` : écoute des messages reçus et transmission à `handleState`
- `handleSends` : envoi des messages et gestion des timeouts de renvoi jusqu'à réception de l'accusé de réception

### Serveur (`server`)

La couche `server` est responsable de l'affichage des messages reçus et de créer les messages à envoyer depuis les
entrées dans `stdin`.

Nous ajouterons néanmoins la gestion du message d'accusé de réception dans la méthode `HandleNetworkMessage` pour
traiter la forme spéciale de `transport.Message` nommée `transport.AckMessage`. Cela permet au serveur d'afficher le
contenu de l'accusé de réception si cela a été demandée dans la configuration. L'appel à cette méthode sera fait par
la goroutine `handleState` de la couche `transport` lors de la réception d'un accusé de réception.
