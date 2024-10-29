# Document d'Architecture Logicielle

Pour permettre d'implémenter la garantie d'ordre total dans le système de serveurs de l'application ChatsApp, nous 
allons proposer une architecture logicielle basée sur les timestamps de Lamport et sur un protocole de consensus.

L'implémentation se traduira en une couche supplémentaire de transport (`transport/mutex`) qui sera utilisée par le
serveur lors de l'entrée dans les sections critiques. Cette couche de transport utilisera une implémentation de
l'horloge de Lamport pour générer des timestamps uniques à chaque message qui transiteront à travers la couche TCP
implémentée précédemment.

L'horloge de Lamport sera implémentée dans un package utilitaire (`utils/lamport`) qui sera utilisé par la couche de
transport pour gérer les timestamps.

## Horloge de Lamport (`utils/lamport`)

Une première abstraction viendra à définir ce qu'une horloge de Lamport devra implémenter.

Nous définissons immédiatement le temps de Lamport comme étant un entier naturel à 64 bits.
```go
package lamport

type LamportTime uint64

func (t LamportTime) LessThan(other LamportTime) bool {
    return t < other
}
```

Avec cette définition, nous pouvons définir une implémentation de l'horloge de Lamport comme étant une structure
répondant à l'interface suivante:
```go
package lamport

// Clock is an interface for Lamport logical clocks
type Clock interface {
	// Time is a getter for the current Lamport time value
    Time() LamportTime
	// Increment increments the Lamport time,
	// returning the new value if successful
    Increment() (LamportTime, error)
	// Witness is called to update the local time if necessary 
	// after witnessing a clock value from another source
    Witness(time LamportTime) error
}
```

De cette interface pourra émerger plusieurs implémentations, nous nous concentrons sur une version simple qui stocke
le compteur en mémoire.

L'état interne revient donc à stocker la valeur du compteur de façon atomique.
```go
package lamport

// LamportClock is a simple implementation of the Lamport logical Clock interface where the time is stored in memory
type LamportClock struct {
    time uint64
}

// NewLamportClock creates a new Clock instance with the timestamp saved in memory
func NewLamportClock() Clock {
    return &LamportClock{time: 1}
}
```

L'algorithmie implémentée par cette horloge reste simple:
- L'horloge est initialisée à 1
- `Time` retourne la valeur actuelle de l'horloge
- `Increment` incrémente l'horloge de 1 et retourne la nouvelle valeur
- `Witness` met à jour l'horloge à la valeur maximale entre la valeur actuelle et la valeur passée en argument et
  incrémente l'horloge de 1.

Pour offrir une garantie de multi-threading, les opérations sur le compteur devront être effectuées de manière atomique.

Cette interface et ses implémentations sont totalement indépendantes de l'implémentation de l'algorithme de Lamport, cela
sert uniquement à simplifier les opérations sur les timestamps de façon concurrente.

## Mutex distribué (`transport/mutex`)

En utilisant l'horloge de Lamport, nous implémenterons une implémentation des mutex distribués dans une couche de
transport qui sera utilisée par les serveurs pour entrer dans les sections critiques. Cette couche utilisera elle la
couche TCP implémentée précédemment pour communiquer avec les autres serveurs.

Un mutex distribué devra au moins répondre à l'interface suivante:
```go
package mutex

type Mutex interface {
    // Lock is called to enter the critical section, it blocks until the lock is acquired
    Lock() error
    // Unlock is called to exit the critical section, it releases for other requests
    Unlock() error
}
```

L'idée sera de permettre à un code appelant, dans notre exemple `server.go`, de pouvoir récupérer le lock lors de
l'envoi d'un broadcast et de le relâcher une fois le broadcast terminé:
```go
package server

func (s *server) broadcast() {
  err := s.mutex.Lock()
  if err != nil {
    // Handle error
  }
  
  defer s.mutex.Unlock()
  // Do critical section
}
```

Ce système pourrait contenir plusieurs implémentations pour les différents algorithmes de mutex distribués. Dans cette
architecture nous fournirons uniquement la version de Lamport.

Nous ajouterons donc l'implémentation concrète `LamportMutex` qui utilisera les horloges de Lamport pour gérer les 
timestamps des messages et l'algorithme de Lamport pour la communication et l'attribution des locks. Chaque instance
de l'implémentation devra être initialisée avec un identifiant unique pour le serveur (`instanceId`) et une liste des
serveurs avec lesquels communiquer (`servers`).

Cette implémentation devra donc avoir accès à l'interface network pour envoyer et recevoir ses messages. Il est aussi
nécessaire de connaitre la liste des serveurs voisins pour allouer le tableau des états et horloges.

Dans l'essentiel, l'implémentation partira avec la configuration suivante, les autres éléments d'état interne seront
décrits plus loin. `NewLamportMutex` devra en outre initialiser les goroutines définies ci-après.
```go
package mutex

type LamportMutex struct {
	instanceId uint64
	servers    []transport.Address
	network    transport.NetworkInterface
}

func NewLamportMutex(
	servers []transport.Address,
	network transport.NetworkInterface,
) Mutex {
	return &LamportMutex{
		instanceId: uint64(time.Now().UnixMilli()),
		servers:    servers,
		network:    network,
	}
}
```

`LamportMutex` devra aussi implémenter l'interface `network.MessageHandler` pour le traitement de requêtes entrantes.

Pour les communications entre les serveurs, nous utiliserons un nouveau type de message spécifique pour notre 
implémentation: `LamportMessage`. Ce message contiendra en outre un type de message similaire à celui du protocole RR.

```go
package mutex

type msgType uint8

const (
	reqMsg msgType = iota
	ackMsg
	relMsg
)

func (m msgType) String() string {
	switch m {
	case reqMsg:
		return "REQ"
	case ackMsg:
		return "ACK"
	case relMsg:
		return "REL"
	default:
		return "UNK"
	}
}
```

En ce qui concerne le message, nous partirons de la définition suivante. Nous y ajoutons un champ `Sender` qui identifie
l'émetteur du message, pour permettre la garantie d'ordre total dans la situation où deux messages de timestamps égaux
arrivent en même temps et doivent être ordonnés. Ces messages seront transférés avec `gob` via la couche TCP associée
au serveur.

```go
package mutex

type lamportMessage struct {
	Type   msgType
	Stamp  lamport.LamportTime
	Sender uint64
}

func newReqLamportMessage(stamp lamport.LamportTime, sender uint64) lamportMessage {
    return lamportMessage{Type: reqMsg, Stamp: stamp, Sender: sender}
}

func newAckLamportMessage(stamp lamport.LamportTime, sender uint64) lamportMessage {
    return lamportMessage{Type: ackMsg, Stamp: stamp, Sender: sender}
}

func newRelLamportMessage(stamp lamport.LamportTime, sender uint64) lamportMessage {
    return lamportMessage{Type: relMsg, Stamp: stamp, Sender: sender}
}
```

Pour stocker les requêtes entrantes, nous utiliserons un tas selon la définition d'ordre `byTotalOrder` garantissant
l'ordre total.
```go
package mutex

type byTotalOrder []lamportMessage

func (a byTotalOrder) Len() int           { return len(a) }
func (a byTotalOrder) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byTotalOrder) Less(i, j int) bool { return a[i].Stamp.LessThan(a[j].Stamp) || (a[i].Stamp == a[j].Stamp && a[i].Sender < a[j].Sender)}
func (a *byTotalOrder) Push(x any)        { *a = append(*a, x.(lamportMessage)) }
func (a *byTotalOrder) Pop() any          { old := *a; n := len(old); x := old[n-1]; *a = old[0 : n-1]; return x }
```

Fort de nos définitions rigoureuses, nous pouvons maintenant définir le fonctionnement de notre implémentation.

### État interne

L'état interne du mutex distribué sera composé des éléments suivants:

- Le timestamp local de l'horloge de Lamport (`lamport.LamportTime`)
- La pile des requêtes en attente (`container/heap` défini avec `byTotalOrder`, `a := &byTotalOrder{}; heap.Init(a)`)

À noter que le tableau des statuts stockera toujours les messages tels que reçus par le réseau ou par le serveur local
dans le cas d'une requête de lock, seulement le timestamp local sera incrémenté par une instance donnée.

Lorsqu'un serveur demande l'accès à la section critique, il passera par la méthode `Lock` qui va devoir communiquer
avec la gestion de l'état pour mettre à jour l'état local. Un channel dedié à cet effet sera créé sur l'instance et
écouté par la goroutine de gestion de l'état.
- `requestAccess := make(chan chan error)` (utilisation incomplète: `waitAccess := make(chan error); lm.requestAccess <- waitAccess; <-waitAccess`)
- `releaseAccess := make(chan struct{})` (pour que `Unlock` puisse signaler la fin de la section critique à la gestion de l'état)

Enfin, nous ajouterons un channel pour les messages entrants (venants de la couche TCP). La méthode 
`HandleNetworkMessage` sera appelée par la couche réseau et si le message est un message de Lamport, il sera traité
par la goroutine correspondante.
- `incomingMessages := make(chan lamportMessage)`

### Goroutine principale (`handleState`)

Une goroutine principale `handleState` sera lancée par le constructeur de `LamportMutex`. Cette goroutine sera chargée
de stocker le timestamp local et la pile des reqûetes. Cela permettra d'éviter des accès concurrents à ces données et
de garantir la cohérence des données.

Cette goroutine devra donc être capable d'envoyer des messages de type `reqMsg` aux autres serveurs lors de l'arrivée
d'une demande depuis `requestAccess`. Cela demandera également d'ajouter la requête dans sa propre pile.
Lors de l'envoi d'un message, la méthode `Increment` de l'horloge de Lamport sera appelée avant de définir
le contenu du `lamportMessage`.

Lors de la réception d'un message de type quelconque, la goroutine devra d'abord appeler la méthode `Witness` de
son horloge de Lamport avec le `lamport.LamportTime` reçu.
- Pour un message de type `REQ`, un message de type `ACK` devra être envoyé en réponse si le serveur local n'est pas
  en attente de l'accès à la section critique. La requête devra être ajoutée à la pile
- Pour un message de type `ACK`, le serveur local n'a rien à faire. La vérification de l'accès à la section critique 
  sera faite juste après.
- Pour un message de type `REL`, le serveur local retirer de la pile la requête du serveur correspondant.

Après chaque réception de message, une tentative d'entrée en section critique est effectuée. Pour pouvoir rentrer en
section critique, il faut simplemement vérifier si la requête en sommet de pile. Dans le cas où ces conditions sont 
remplies, le serveur local ouvre la section critique et le signalera via le channel `waitAccess`.

Lors de l'appel à la méthode `Unlock`, la goroutine `handleState` devra envoyer un message de type `REL` à tous les
serveurs voisins et retirer la requête de sa propre pile. Cela incrémentera également l'horloge de Lamport sur serveur
local lors de la création des messages `REL` associés.

## Modification du serveur

Afin de garantir l'ordre total de l'affichage des messages entre les serveurs, nous devons modifier le serveur pour
qu'il utilise notre implémentation de mutex. Pour ce faire, nous allons initialiser l'instance du mutex de Lamport dans
le serveur une fois la couche TCP créée.

Ensuite, il s'agira de protéger la méthode `broadcast` avec l'obtention du lock selon l'exemple présenté plus haut.
Cela permettra de s'assurer que l'envoi d'un message soit effectué que par un serveur à la fois et que aucun autre
message ne soit envoyé avant que le message courant ne soir reçu par tous les serveurs.
