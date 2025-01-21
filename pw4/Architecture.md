# Document d'Architecture Logicielle

## Pulsar

Pour simplifier la gestion des retours, nous définissons une abstraction `result.ErrorOr[M]` qui permet de gérer les
valeurs multiples de retours de façon plus simple. Cette abstraction se rapproche beaucoup des types `Result` de Rust.
Nous utiliserons ce type de retour pour le channel qui est ouvert lors du démarrage d'une sonde en attente d'un
résultat, où dans le cas où une erreur est survenue le type est "unwrap" en un tuple `(nil, error)`.

L'état interne du pulsar est stocké dans une map organisée par ID de sonde, sur laquelle on définit plusieurs méthodes
utilitaires pour instancier l'état pour la source ou pour un echo entrant. Cet état est stocké dans la seule goroutine
du pulsar et son traitement étant séquentiel, il ne peut pas y avoir d'accès concurrents.

### Goroutines et channels

Nous définissons un channel principal `pulseRequests` qui est utilisé pour traiter de manière séquentielle les requêtes
de sondes. Ce channel prend en paramètre un channel secondaire qui retourne un `result.ErrorOr[M]`. Ce channel
secondaire devra être attendu par la méthode `StartPulse` pour récupérer le résultat de la sonde.

Le pulsar utilise une goroutine `handlePulses` qui reçoit la partie sortante du channel `pulseRequests` et qui traitera
la requête. `handlePulses` écoute aussi le channel entrant `netToPulsar` pour traiter les sondes et échos entrants.

Le traitement des événements entrants depuis les deux channels est fait de manière séquentielle, événement par
événement. Si des traitements sont encore en cours, les événements entrants sont mis en attente. Garantissant ainsi
qu'il n'y aura pas de problèmes de concurrence.

Le pulsar utilise un channel sortant `pulsarToNet` pour envoyer les différents types de messages sortants. Ce channel
comme le channel entrant `netToPulsar` est fourni par le créateur du pulsar via son builder, plus loin ce sera le
dispatcher qui fera l'instantiation et la gestion de ces channels.

## Router

Pour gérer la table de routage de manière concurrente, nous utilisons une abstraction supplémentaire qui s'ocucpe de la
gestion des accès à la map sous-jacente. Une goroutine `handleTable` est lancée à l'instantiation du handler et trois
channels sont utilisés pour communiquer avec cette goroutine.

- `getReq` permet de récupérer une copie complète de la table, le résultat est retourné via un channel secondaire fourni
  en paramètre.
- `lookupReq` permet de rechercher dans la table le prochain saut pour une destination donnée, le résultat est retourné
  dans une `option.Option` via un channel secondaire fourni en paramètre. L'option est vide si la destination n'est pas
  trouvée.
- `updateReq` permet de synchroniser la table avec une liste de routes fournies, un channel vide est utilisé pour
  signaler la fin de l'opération.

Ces trois channels principaux et leurs partie secondaire sous-jacentes sont utilisés par des méthodes bloquantes sur le
handler qui elles-mêmes pourront être appelées par le router lors des besoins d'accès à la table de routage.

### Goroutines et channels

Fort de notre abstraction pour la gestion sans data-race de la table de routage, nous définissons maintenant deux
goroutines principales pour le router.

- `handleSendRequests` gère les demandes d'envoi reçues par un channel à buffer infini `sendRequests`. Cette goroutine
  va traiter les demandes d'envois d'un message ciblé. Si une route n'est pas trouvée, une exploration est lancée pour
  tenter de la trouver. L'exploration via le pulsar étant bloquante, la goroutine ne traitera pas d'autres messages
  sortants tant que ce n'est pas terminé et que le message a pu être envoyé (ou non si la route n'est pas trouvée). On
  garantit par ce principe la propriété d'ordre des messages nécessaire pour le bon fonctionnement du reste du système.
- `handleIncomingMessages` traite les messages entrants du réseau destinés au routeur, via le channel `netToRouter`.
  Ce channel est fourni à la création du routeur par la couche supérieure (le dispatcher). Les messages entrants sont
  soit des messages destinés au processus actifs (communication envoyée par le router d'un autre processus), dans ce cas
  il est transmis via un channel à buffer infini `receivedMessages` qui est lu par le code appelant
  `ReceivedMessageChan` (plus loin, le dispatcher qui le traitera). Soit le message est un message destiné à un autre
  processus, dans ce cas il est transmis à la goroutine `handleSendRequests` via le channel `sendRequests`. Ce message
  sera ensuite forwardé par le routeur vers le noeud suivant, potentiellement la cible originale du message.

Les demandes d'envoi sont effectuées en donnant un channel secondaire sur lequel la méthode `Send` pourra récupérer le
résultat de l'envoi. La méthode `Send` effectuera donc une attente sur ce résultat ce qui aura pour effet de bloquer
le code appelant tant que le message n'a pas été correctement envoyé (ou qu'une erreur est remontée le cas échéant).

Les messages sortants du router (ceux qui ne passent pas par le pulsar, c'est-à-dire les messages addressés via la
table de routage), sont envoyés via un channel `routerToNet` fourni par le créateur du router via son constructeur.

Les deux goroutines étant séparées, le traitement des messages entrants pourra continuer même si le traitement des
messages sortants est bloqué par une recherche de route. Les messages sortants seront mis en file d'attente et seront
traités une fois l'exploration du premier message sortant terminée. Si tout va bien, le système devrait à ce point être
capable de router un message pour tous les noeuds du réseau.

## Dispatcher

Pour faire les liens réseaux du pulsar et du router, le dispatcher initialise à la création deux paires de channels
`pulsarToNet` et `netToPulsar` pour le pulsar et `routerToNet` et `netToRouter` pour le router. Les channels vers
les composants contiennent un buffer infini, pour permettre de compenser les temps de traitement des demandes en cas
de besoin d'exploration.

On enregistre ensuite un handler pour les messages entrants du pulsar et du router qui seront transmis depuis le réseau
vers le composant approprié.

Une goroutine supplémentaire, `handleMessageProxying`, est démarrée et elle gérera le forwarding des messages entrants
donnés par le router via le channel `ReceivedMessageChan` pour qu'ils soient transmis à la couche applicative. Cette
goroutine traite aussi l'envoi sur le réseau des messages donnés par les channels `pulsarToNet` et `routerToNet`.
