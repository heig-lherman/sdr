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

type LamportTime uint32

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
    Increment() LamportTime
	// Witness is called to update the local time if necessary 
	// after witnessing a clock value from another source
    Witness(time LamportTime)
}
```

De cette interface pourra émerger plusieurs implémentations, nous nous concentrons sur une version simple qui stocke
le compteur en mémoire.

L'état interne revient donc à stocker la valeur du compteur de façon atomique.
```go
package lamport

// LamportClock is a simple implementation of the Lamport logical Clock interface where the time is stored in memory
type LamportClock struct {
    time uint32
}

// NewLamportClock creates a new Clock instance with the timestamp saved in memory
func NewLamportClock() Clock {
    return &LamportClock{time: 0}
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
  /*
   * Request permission to enter critical section.
   *
   * Returns a channel that should be closed by the critical section when it is done, to signal that the mutex can be released.
   *
   * It is recommended to defer the closing of the channel immediately after acquiring the mutex to avoid deadlocks.
   */
  Request() (release func(), err error)
}

```

L'idée sera de permettre à un code appelant, dans notre exemple `server.go`, de pouvoir récupérer le lock lors de
l'envoi d'un broadcast et de le relâcher une fois le broadcast terminé:
```go
package server

func (s *server) broadcast() {
  release, err := s.mutex.Request()
  if err != nil {
    // Handle error
  }
  defer release()
  
  // Do critical section
}
```

Ce système pourrait contenir plusieurs implémentations pour les différents algorithmes de mutex distribués. Dans cette
architecture nous fournirons uniquement la version de Lamport.

Nous ajouterons donc l'implémentation concrète `LamportMutex` qui utilisera les horloges de Lamport pour gérer les
timestamps des messages et l'algorithme de Lamport pour la communication et l'attribution des locks. Chaque instance
de l'implémentation devra être initialisée avec un identifiant unique pour le serveur (`Pid`) et une liste des
serveurs avec lesquels communiquer (`neighborPids`). Les `Pid` doivent être uniques pour chaque serveur, dans le code
de production ces identifiants corresponderont aux adresses IP des serveurs.

Cette implémentation devra donc avoir accès à l'interface network pour envoyer et recevoir ses messages. Il est aussi
nécessaire de connaitre la liste des serveurs voisins pour allouer le tableau des états et horloges.

Dans l'essentiel, l'implémentation partira avec la configuration définie ci-avant, les autres éléments d'état interne
seront décrits plus loin. `NewLamportMutex` devra en outre initialiser les goroutines définies ci-après.

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

Un `timestamp` contiendra le timestamp de Lamport associé au message et le `Pid` de la machine qui a envoyé le message.

```go
package mutex

type LamportMessage struct {
	Type   msgType
	Stamp  timestamp
}
```

Pour stocker les status de chaque serveur, nous utiliserons une structure heap-map qui permettra de stocker les états
de chaque serveur dans un tableau avec un index sous forme de tas trié selon la définition d'ordre total basé sur les
horloges de Lamport et le PID des serveurs.

Fort de nos définitions rigoureuses, nous pouvons maintenant définir le fonctionnement de notre implémentation.

### État interne

L'état interne du mutex distribué sera composé des éléments suivants:

- L'horloge de lamport locale (`lamport.Clock`)
- Le heapmap des états des serveurs voisins (`state := utils.NewHeapMap[Pid, messageType, timestamp]`) initialisé avec
  pour chaque serveur distant un timestamp à 0 et un état `REL`.

À noter que la pile des requêtes stockera toujours les messages tels que reçus par le réseau ou par le serveur local
dans le cas d'une requête de lock, seulement le timestamp local sera incrémenté par une instance donnée.

Lorsqu'un serveur demande l'accès à la section critique, il passera par la méthode `Request` qui va devoir communiquer
avec la gestion de l'état pour mettre à jour l'état local. Un channel dedié à cet effet sera créé sur l'instance et
écouté par la goroutine de gestion de l'état.
- `requestAccess := make(chan chan error)` (utilisation incomplète: `waitAccess := make(chan error); lm.requestAccess <- waitAccess; <-waitAccess`)
- `releaseAccess := make(chan struct{})` (pour que `Unlock` puisse signaler la fin de la section critique à la gestion de l'état)

### Goroutine principale (`handleState`)

Une goroutine principale `handleState` sera lancée par le constructeur de `LamportMutex`. Cette goroutine sera chargée
de stocker le timestamp local et la pile des requêtes. Cela permettra d'éviter des accès concurrents à ces données et
de garantir la cohérence des données.

Cette goroutine devra donc être capable d'envoyer des messages de type `reqMsg` aux autres serveurs lors de l'arrivée
d'une demande depuis `requestAccess`. Cela demandera également d'ajouter la requête dans sa propre pile.
Lors de l'envoi d'un message, la méthode `Increment` de l'horloge de Lamport sera appelée avant de définir
le contenu du `lamportMessage`.

Lors de la réception d'un message de type quelconque, la goroutine devra d'abord appeler la méthode `Witness` de
son horloge de Lamport avec le `lamport.LamportTime` reçu et ensuite stocker le message dans le tableau selon le
fonctionnement de l'algorithme.
- Pour un message de type `REQ`, un message de type `ACK` devra être envoyé. 
- Pour un message de type `ACK`, le serveur n'a rien à faire de plus. Un ACK ne doit pas par contre pas être stocké pour
  un serveur qui a effectué précédemment un `REQ`.
- Pour un message de type `REL`, le serveur n'a rien à faire de plus.

Après chaque réception de message, une tentative d'entrée en section critique est effectuée. Pour pouvoir rentrer en
section critique, il faut simplemement vérifier si la requête en sommet de pile correspond au serveur local. 
Dans le cas où ces conditions sont remplies, le serveur local ouvre la section critique et le signalera via le channel
`waitAccess`, en retournant qu'il n'y a eu aucune erreur lors de l'attente.

Lors de l'appel à la méthode `release`, la goroutine `handleState` devra envoyer un message de type `REL` à tous les
serveurs voisins et retirer la requête de sa propre pile. Cela incrémentera également l'horloge de Lamport sur serveur
local pour la création des messages `REL` associés.

## Modification du serveur

Afin de garantir l'ordre total de l'affichage des messages entre les serveurs, nous devons modifier le serveur pour
qu'il utilise notre implémentation de mutex. Pour ce faire, nous allons initialiser l'instance du mutex de Lamport dans
le serveur une fois la couche TCP créée.

Cela utilisera le principe de dispatcher ainsi qu'une goroutine minime qui écoutera les messages que le mutex souhaite
envoyer pour les transférer au dispatcher sous forme d'envoi.

Ensuite, il s'agira de protéger la méthode `broadcast` avec l'obtention du lock selon l'exemple présenté plus haut.
Cela permettra de s'assurer que l'envoi d'un message soit effectué que par un serveur à la fois et que aucun autre
message ne soit envoyé avant que le message courant ne soir reçu par tous les serveurs.
