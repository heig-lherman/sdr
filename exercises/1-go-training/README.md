# Waste Management

Implémenter en Go l'interface `WasteManager` fournie, responsable de

- Recevoir un déchet ("scrap") à traiter avec sa méthode `Process`,
- Déterminer s'il est recyclable ou non à l'aide d'un critère `RecyclingCriterion` fourni,
- S'il est recyclable,
    - le recycler à l'aide d'une fonction `Recycler` fournie, puis
    - le retourner au prochain appel à `NextRecycledGood`.
- Sinon, le retourner en tant qu'ordure ("waste") au prochain appel à `NextWaste`.

Toutes ces méthodes doivent pouvoir être appelées de manière concurrente.

### Difficulté bonus

- Supposez que le recyclage prend un certain temps, et permettez au traitement d'ordures de pouvoir continuer pendant ce temps.
- Garantissez que les objets recyclés et les ordures sont retournées dans l'ordre dans lequel les déchets correspondants sont reçus. Par exemple
  ```go
    d.Process("good1")
    d.Process("good2")
    d.NextRecycledGood() // good1
    d.NextRecycledGood() // good2

    d.Process("waste1")
    d.Process("waste2")
    d.NextWaste() // waste1
    d.NextWaste() // waste2
  ```
- Garantissez qu'un nombre arbitraire de déchets peuvent être envoyés à `Process` avant que `NextRecycledGood` ou `NextWaste` ne soient appelés.
