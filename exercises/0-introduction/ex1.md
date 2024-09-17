Solution 1
==========

|            | Absence traitement                           | Duplication traitement                                    | Absence confirmation                                            | Duplication confirmation                                 |
|-----------:|----------------------------------------------|-----------------------------------------------------------|-----------------------------------------------------------------|----------------------------------------------------------|
|          R | Oui                                          | impossible: le client n'envoie jamais en double           | inapplicable: le client de reçoit jamais de confirmation        | inapplicable: le client ne reçoit jamais de confirmation |
|         RR | impossible: le client réessaie après timeout | Oui                                                       | impossible: le client réessaie après timeout                    | possible, mais pas à cause d'une perte de message        |
| RR avec ID | impossible: le client réessaie après timeout | impossible: l'id assure l'absence de doublon côté serveur | Oui, pas de duplication des traitements mais requêtes en double | impossible: l'identificateur supprime les doublons       |
|        RRA | impossible: le client réessaie après timeout | impossible: l'id assure l'absence de doublon côté serveur | Oui, pas de duplication des traitements mais requêtes en double | impossible: l'identificateur supprime les doublons       |
