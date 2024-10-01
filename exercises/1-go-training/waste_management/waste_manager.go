package dispatcher

type Scrap string
type Waste string
type RecycledGood string

// Function that determines if a scrap is recyclable. Also returns, if the scrap is not recyclable, the waste it becomes.
type RecyclingCriterion func(Scrap) (isRecyclable bool, asWaste Waste)

// Function that recycles a scrap into a RecycledGood.
type Recycler func(Scrap) RecycledGood

type WasteManager interface {
	ChangeRecyclingCriterion(RecyclingCriterion)
	ChangeRecycler(Recycler)

	Process(Scrap)
	NextRecycledGood() RecycledGood
	NextWaste() Waste
}

type wasteRequest struct {
	result chan Waste
}

type recycledGoodRequest struct {
	result chan RecycledGood
}

type wasteManager struct {
	isRecyclable RecyclingCriterion
	recycler     Recycler

	requestScrap        chan Scrap
	requestWaste        chan wasteRequest
	requestRecycledGood chan recycledGoodRequest
}

func NewWasteManager(isRecyclable RecyclingCriterion, recycler Recycler) WasteManager {
	wm := wasteManager{
		isRecyclable: isRecyclable,
		recycler:     recycler,

		requestScrap:        make(chan Scrap),
		requestWaste:        make(chan wasteRequest),
		requestRecycledGood: make(chan recycledGoodRequest),
	}

	go wm.handleState()

	return &wm
}

func (wm *wasteManager) ChangeRecyclingCriterion(isRecyclable RecyclingCriterion) {
	wm.isRecyclable = isRecyclable
}

func (wm *wasteManager) ChangeRecycler(recycler Recycler) {
	wm.recycler = recycler
}

func (wm *wasteManager) Process(scrap Scrap) {
	wm.requestScrap <- scrap
}

func (wm *wasteManager) handleState() {
	processingGoods := make([]chan RecycledGood, 0)
	processingWaste := make([]chan Waste, 0)

	for {
		select {
		case scrap := <-wm.requestScrap:
			if isRecyclable, asWaste := wm.isRecyclable(scrap); isRecyclable {
				result := make(chan RecycledGood)
				processingGoods = append(processingGoods, result)
				go func() {
					result <- wm.recycler(scrap)
				}()
			} else {
				result := make(chan Waste)
				processingWaste = append(processingWaste, result)
				go func() {
					result <- asWaste
				}()
			}
		case request := <-wm.requestRecycledGood:
			ch := processingGoods[0]
			processingGoods = processingGoods[1:]
			request.result <- <-ch
		case request := <-wm.requestWaste:
			ch := processingWaste[0]
			processingWaste = processingWaste[1:]
			request.result <- <-ch
		default:
		}
	}
}

func (wm *wasteManager) NextRecycledGood() RecycledGood {
	result := make(chan RecycledGood)
	wm.requestRecycledGood <- recycledGoodRequest{result}
	return <-result
}

func (wm *wasteManager) NextWaste() Waste {
	result := make(chan Waste)
	wm.requestWaste <- wasteRequest{result}
	return <-result
}
