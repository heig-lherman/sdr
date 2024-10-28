# Document d'Architecture Logicielle

## Structure générale

Nous allons reprendre la stucture fournie dans le projet de base, en y ajoutant les implémentations nécessaires pour
prendre en charge les garanties demandées. Nos modifications comprendront l'ajout du protocole de transport TCP pour
remplacer UDP et garantir l'ordre d'arrivée ainsi que la détection de perte de paquets.

## Couches logicielles

### Protocole RR (`transport/rr`)

Le protocole RR est implémenté séparément de la couche réseau via la structure `rr.RR` qui utillise un objet
`RRNetWrapper` pour encapsuler les flux entrants et sortant du flux réseau associé. Cela permet de le rendre 
indépendant de la couche réseau, ce qui simplifie les tests.

#### État interne

La gestion des envois ne demande pas de stockage d'état interne, par contre la gestion des réceptions nécessite 
les éléments suivants :

- le dernier numéro de séquence envoyé ainsi que son payload associé
- stockage des éléments modifiables de l'instance (slowdown et request handler)

#### Goroutines principales

Les processus principaux du protocoles sont séparés en trois goroutines:

- `processSends`: envoi des messages au serveur distant et gestion des timeouts avant réception des réponses
- `processReplies`: gestion des requêtes entrantes d'un serveur distant et envoi des confirmations de réception
- `listenIncoming`: écoute des messages entrants de la couche réseau pour trier selon le type de message (requête/réponse)

Lors de l'envoi d'un message via la méthode `SendRequest` de l'interface `RR`, une demande d'envoi est transmise à
la goroutine `processSends` qui traitera la demande une fois que toutes les demandes précédentes sont traitées. 
`SendRequest` est donc bloquant jusqu'à ce que le message soit envoyé et qu'une confirmation de réception est reçue,
le résultat ou une erreur sera retournée en fonction de la réussite de l'envoi.

Lors du traitement d'une demande, un numéro de séquence est généré pour ce message et est envoyé à la couche réseau
pour transfert. Ensuite le processus de timeouts est démarré et tant qu'une réponse n'est pas reçue, le message sera
retransmis à intervalles réguliers. Le résultat sera retourné à `SendRequest` et débloquera une requête suivante 
s'il y en a.

Pour le traitement des messages entrants de la couche réseau, la goroutine `listenIncoming` va déserialiser les messages
et en fonction du type (requête ou réponse) les transmettre à la goroutine correspondante. En cas de requête, 
une réponse doit être générée par `processReplies`. En cas de réponse, le message est transmis à `processSends` pour
vérifier la validité de la réponse et retourner son contenu à l'expéditeur. À noter que si le numéro de séquence du
message reçu est inférieur, ce dernier sera ignoré.

`processReplies` va, à la réception d'une requête, vérifier le numéro de séquence avec celui gardé en mémoire et effectuer
l'action suivante.
- En cas de numéro de séquence inférieur, le message est ignoré.
- Si le numéro est identique à celui stocké, le payload existant est retourné.
- Sinon, le message est transmis au request handler et le payload stocké est mis à jour pour la requête suivante.

### Réseau (`transport`)

En partant de l'abstraction `NetworkInterface`, nous offrons une nouvelle implémentation, `TCP`, largement inspirée de
l'implémentation `UDP` fournie. Cette nouvelle implémentation sera conceptualisée comme présenté ci-après.

Le protocole TCP que nous implémenteront devra en outre garantir que les messages envoyés et reçus le soient exactement
une fois, en ajoutant donc une gestion d'identification des messages correct au sens des pannes.

#### État interne

Similairement à `UDP`, `TCP` maintiendra un état interne pour chaque connexion. Cet état sera composé des éléments
suivants :

- Le listener TCP qui accepte les connexions entrantes
- La liste des voisins connus et leur gestionnaire de connexion (qui contiennent la connexion et l'instance de RR)
- La liste des `MessageHandler` souscrits aux messages reçus

#### Goroutines principales

La couche `TCP` doit traiter les processus ainsi que leurs modifications d'états potentielles suivants :

- **Ajout / retrait de handlers** : ajouter ou retirer des `MessageHandler`, modifie la liste des `MessageHandler`
- **Envoi de messages** : envoyer des messages à un voisin, accède à la liste des gestionnaire de connexion
- **Réception de messages** : accepter les connexions entrantes, accède à la liste des `MessageHandler`
- **Ouverture des connexions** : au démarrage, connexion à tous les voisins spécifiés et ajout à la liste des gestionnaire de connexion
- **Clôture d'une connexion** : fermer une connexion, retire la connexion de la liste des voisins et la connexion de l'écoute des messages.

Nous utiliserons donc une goroutine principale pour gérer l'état de la connexion TCP que nous nommerons `handleConnections`.
Cela permettra d'avoir un gestionnaire unique pour l'état interne qui traitera les événements de manière séquentielle,
via un stockage dans des variables locales à la goroutines pour éviter des modifications externes indésirables.
Ce gestionnaire aura donc une liste de gestionnaires de connexion (`connectionHandler`) qui eux gérent l'objet `net.Conn`
et l'instance de `RR` associée à la connexion.

L'écoute des connexions entrantes se fera dans une goroutine séparée, `listenIncomingConnections`, qui sera lancée lors de 
l'instantiation de l'interface réseau. Cette goroutine s'occupera d'instancier les `connectionHandler` pour chaque connexion
et de transmettre l'enregistrement de la connexion à la goroutine `handleConnections`. Pour qu'une connexion soit acceptée,
on attend un message "handshake", qui est simplement un message vide avec la source définie pour déterminer quelle est
l'adresse du voisin. Une fois le handshake reçu, la connexion est considérée comme établie et ajoutée à la liste des
`connectionHandler`.

Lors de l'instantiation de l'interface réseau, chaque voisin défini qui a un port inférieur à celui du serveur courrant
devra ouvrir la connection au serveur distant via une goroutine `connectNeighbors` qui prendra la liste des adresses et
un channel pour indiquer la fin de l'ouverture des connexions. Cette goroutine va aussi attendre que les voisins dont le
port est supérieur au port du serveur courant ouvrent leurs connexions respectives. Cela permet de garantir que tous les
voisins sont connectés entre eux avant de commencer les échanges de messages.

L'attente de connexion entrante se fera via une liste de channels qui attendent un `connectionHandler` en retour.
Quand une connexion est acceptée et ajoutée à la liste des `connectionHandler`, tous les subscribers qui attendent cette 
connexion seront notifiés et résoudront donc l'attente.

Pour commencer les échanges de messages, la goroutine `handleSendRequests` sera démarrée seulement une fois que tous les
serveurs voisins déclarés seront connectés. Cela permet de garantir qu'il n'y aura pas de double connexions entre tout
deux serveur lors de l'initialisation du système.

Lors de la demande d'envoi de message, la goroutine `handleSendRequests` va chercher le `connectionHandler` associé au
voisin demandé. Si la connexion n'est plus active, une tentative de reconnexion sera effectuée. La méthode `Send` de la
couche réseau transmet également un channel de réponse de type `error` et bloquera jusqu'à ce que la réponse soit reçue.
Le traitement d'une requête revient essentiellement à la transmettre au système `RR` qui définira les messages à envoyer
via les channels fournis par le gestionnaire de connexion. Le gestionnaire écoutera ces channels et effectura l'envoi
via `net.Conn` de la connexion TCP.

Pour les messages reçus et la gestion des `MessageHandler`, la goroutine `handleHandlers` est également démarrée lors
de l'initialisation de l'interface réseau. Cette goroutine va écouter les messages entrants décodés par le gestionnaire
de connexion et les transmettre à tous les `MessageHandler` enregistrés. Les `MessageHandler` peuvent alors traiter 
les messages reçus et effectuer des actions en conséquence.

### Serveur (`server`)

La couche `server` est responsable de l'affichage des messages reçus et de créer les messages à envoyer depuis les
entrées dans `stdin`.

Pour la gestion de l'affichage des confirmations de réception, nous allons utiliser le principe que la fonction `Send`
de la couche `network` telle qu'implémentée par TCP est bloquante. Ainsi, si la méthode ne retourne pas d'erreur, nous
pouvons simplement afficher le message dans la console.