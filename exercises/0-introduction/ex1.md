Solution 1
==========

> Supposez que le réseau ne nous garantit plus l'absence de perte de message. Quelles sont alors les garanties données par les différentes classes de fiabilité ? Pour répondre, notez pour chacun des 4 protocoles, s'il y a ou non risque de
> - absence de traitement d'une requête
> - duplication de traitement d'une requête
> - absence de traitement d'une requête
> - duplication de confirmation de traitement
> et si oui, un exemple de scénario qui peut le causer.
>
> Si de nouveaux problèmes surgissent pour RR avec id et RRA, quelle solution proposez-vous ?


|            | Absence traitement                           | Duplication traitement                                    | Absence confirmation                                            | Duplication confirmation                                 |
|-----------:|----------------------------------------------|-----------------------------------------------------------|-----------------------------------------------------------------|----------------------------------------------------------|
|          R | Oui                                          | impossible: le client n'envoie jamais en double           | inapplicable: le client de reçoit jamais de confirmation        | inapplicable: le client ne reçoit jamais de confirmation |
|         RR | impossible: le client réessaie après timeout | Oui                                                       | impossible: le client réessaie après timeout                    | possible, mais pas à cause d'une perte de message        |
| RR avec ID | impossible: le client réessaie après timeout | impossible: l'id assure l'absence de doublon côté serveur | Oui, pas de duplication des traitements mais requêtes en double | impossible: l'identificateur supprime les doublons       |
|        RRA | impossible: le client réessaie après timeout | impossible: l'id assure l'absence de doublon côté serveur | Oui, pas de duplication des traitements mais requêtes en double | impossible: l'identificateur supprime les doublons       |
