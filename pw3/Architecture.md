# Document d'Architecture Logicielle

## Ring maintainer

Le ring maintainer ne définit pas plus d'abstractions mais utilise la structure de donnée `container/ring.Ring` de Go
pour simplifier la gestion des serveurs selon l'ordre donné tout en permettant un parcours circulaire.

On initialise ce ring selon la liste donnée dans le constructeur du ring maintainer, lors de l'envoi d'un message on
vérifiera à chaque fois que le ring est bien positionné sur le serveur courant, pour garantir que le message soit bien
envoyé au serveur suivant et au serveur courant en dernier.

### Goroutines

Le ring maintainer utilise deux goroutines.

La première, `handleState` pour gérer son état interne, comportant:

- Le ring des serveurs
- Un timestamp handler pour identifier les messages sortants

Via deux channels cette goroutine reçoit les demandes d'envois de messages et les demandes d'attente d'un message.
Dans les deux cas la goroutine `handleState` démarrera une goroutine interne pour gérer les actions.

1. Dans le cas d'un envoi de message, la goroutine interne enverra le message et va démarrer le timeout selon les règles
   de l'algorithme. Une copie du ring est transmise à la goroutine pour qu'elle puisse itérer en cas de pannes, sans que
   cela ne décale le ring pour les autres messages.
2. Dans le cas d'une attente de message, la goroutine interne va attendre le message et le renvoyer via un channel à la
   méthode appelante, en général `ReceiveFromPrev`.

L'écoute des messages entrants se fait via le `dispatcher.Dispatcher` qui est injecté dans le ring maintainer,
les messages seront triées en entrée et envoyés via des channels au bon destinataire:

1. Les messages de type `ACK` sont envoyés dans la gestion du timeout pour l'annuler
2. Les messages de type `MSG` sont envoyés via `handleState` pour être traités par `ReceiveFromPrev`

La deuxième goroutine, `processSends`, est utilisée pour envoyer les messages sortants, elle est utilisée par
`handleState` lors de l'envoi de messages. Cette goroutine permet de n'avoir qu'à gérer un seul message à envoyer à la
fois, et donc s'occupe de réceptionner les `ACK` en retour.

### Channels

Au niveau de la structure du ring maintainer, on utilise plusieurs channels pour communiquer entre les différentes
goroutines:

| Channel                                  | Utilisation                                                                      |
|------------------------------------------|----------------------------------------------------------------------------------|
| `sendMsg chan dispatcher.Message`        | permet de demander l'envoi d'un message                                          | 
| `waitMsg chan chan<- dispatcher.Message` | permet de demander l'attente d'un message en envoyan un channel pour le résultat |
| `incAck     chan incomingMessage`        | depuis le message handler pour les ACKS gérés par `processSends`                 |
| `incPayload chan incomingMessage`        | depuis le message handler pour les messages gérés par `handleState`              |
| `outMsg     chan outgoingMessage`        | depuis `handleState` pour envoyer les messages sortants à `processSends`         |

## Électeur de Chang-Roberts

L'électeur de Chang-Roberts n'utilise pas d'autres abstractions que celle du `ring.RingMaintainer`.

### Goroutines

L'électeur de Chang-Roberts utilise deux goroutines.

La première `handleRingMessages` est démarrée dans le constructeur de l'électeur et est utilisée pour réceptionner les
messages depuis le `ring.RingMaintainer` et les traiter selon leurs types en les envoyant via des channels dédiés.

La seconde, `processElections` est la goroutine principale de l'électeur, elle est démarrée dans le constructeur de
l'électeur et est utilisée pour stocker l'état de l'électeur et gérer les élections selon l'algorithme.

L'état interne de cette dernière est défini comme suit:

- Le leader actif, stocké via un pointeur initialisé à une valeur-zero
- L'aptitude, initialisée à 0
- Le statut de l'élection, initialisé à `false`
- Une liste de callbacks qui seront appelés lorsqu'un nouveau leader est élu, dans le cas où une élection est en cours.

### Channels

La goroutine `processElections` applique le fonctionnement de l'algorithme de Chang-Roberts en utilisant les channels
suivants pour représenter les évènements.

| Channel                                    | Utilisation                                                                                                                                                                                    |
|--------------------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `getLeader  chan chan<- address`           | permet de récupérer le leader actuel, une élection est lancée si aucun leader n'est présent, en cas d'élection le channel donné n'aura une valeur de retour seulement quand l'élection sera terminée |
| `newAbility chan newAbilityRequest`        | permet de mettre à jour l'aptitude de l'électeur, déclenche une nouvelle élection.                                                                                                             |
| `incAnnouncement chan announcementMessage` | arrivée d'un message d'annonce depuis le noeud précédent                                                                                                                                       |
| `incResult       chan resultMessage`       | arrivée d'un message de résultat depuis le noeud précédent                                                                                                                                     |

## Gestion des clients

L'électeur étant déjà intégré dans le système, nous mettons donc juste à jour la réponse du serveur pour les demandes
de connexions des clients en utilisant la méthode `GetLeader()` de l'électeur.

```go
leader := m.elector.GetLeader()
m.logger.Infof("Responding to conn req from %s with leader %s", source, leader)
m.dispatcher.Send(common.ConnResponseMessage{Leader: leader}, source)
```

On ajoute aussi la condition de l'ajout du client dans la map des clients si et seulement si
le serveur courant est le leader, dans ce cas on met aussi à jour sont aptitude.

```go
if leader == m.self {
   clients[source] = clientConnection{source: source, user: clientMsg.User}
   m.elector.UpdateAbility(-len(clients))
}
```

> [!NOTE]
> On définira l'aptitude dans le contexte de l'élection utilisée par le système comme étant l'opposé du nombre
> de clients connectés au serveur courant, cela permettra de donner la priorité aux serveurs ayant le moins de clients,
> car leur aptitude sera par conséquent plus élevée.

Quand un client se déconnecte, une mise à jour de l'aptitude est effectuée en utilisant la même méthode.
