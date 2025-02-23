package blockchainv3

import (
	"github.com/hacash/chain/blockstorev3"
	"github.com/hacash/chain/chainstatev3"
	"github.com/hacash/core/interfaces"
	"github.com/hacash/core/stores"
	"github.com/hacash/core/sys"
	"github.com/hacash/mint/event"
	"sync"
)

const (
	ImmatureBlockMaxLength   = 4 // Up to four immature blocks are allowed
	block_time_format_layout = "01/02 15:04:05"
)

////////////////////////////////////////////////

type ChainKernelConfig struct {
	cnffile          *sys.Inicnf
	RollbackToHeight uint64
	Datadir          string
}

func NewEmptyChainKernelConfig() *ChainKernelConfig {
	cnf := &ChainKernelConfig{
		RollbackToHeight: 0,
	}
	return cnf
}

func NewChainKernelConfig(cnffile *sys.Inicnf) *ChainKernelConfig {
	cnf := NewEmptyChainKernelConfig()
	cnf.cnffile = cnffile
	section := cnffile.Section("")
	cnf.RollbackToHeight = section.Key("RollbackToHeight").MustUint64(0)
	cnf.Datadir = cnffile.MustDataDirWithVersion()
	return cnf

}

////////////////////////////////////

type ChainKernel struct {
	config *ChainKernelConfig

	initcall func(interfaces.ChainStateOperation)

	//状态
	stateImmutable *chainstatev3.ChainState
	stateCurrent   *chainstatev3.ChainState

	blockstore *blockstorev3.BlockStore

	// feed
	validatedBlockInsertFeed *event.Feed
	diamondCreateFeed        *event.Feed

	insertLock *sync.RWMutex
}

func NewChainKernel(cnf *ChainKernelConfig) (*ChainKernel, error) {

	stocnf := blockstorev3.NewBlockStoreConfig(cnf.cnffile)
	blockstore, e := blockstorev3.NewBlockStore(stocnf)
	if e != nil {
		return nil, e
	}

	scnf := chainstatev3.NewChainStateConfig(cnf.cnffile)
	immutable, e := chainstatev3.NewChainStateImmutable(scnf)
	if e != nil {
		return nil, e
	}

	// Create Peading status
	last, e := immutable.ImmutableStatusRead()
	if e != nil {
		immutable.Close()
		return nil, e
	}
	curblk := last.GetImmutableBlockHeadMeta()
	e = immutable.SetPending(chainstatev3.NewPendingStatus(0, nil, curblk))
	if e != nil {
		immutable.Close()
		return nil, e
	}

	// Block Storage 
	immutable.SetBlockStoreObj(blockstore)

	ins := &ChainKernel{
		config:                   cnf,
		initcall:                 nil,
		stateCurrent:             nil,
		stateImmutable:           immutable,
		blockstore:               blockstore,
		validatedBlockInsertFeed: &event.Feed{},
		diamondCreateFeed:        &event.Feed{},
		insertLock:               &sync.RWMutex{},
	}

	// Rebuild immature block status and return the latest block value
	ins.stateCurrent, e = ins.BuildImmatureBlockStates()
	if e != nil {
		return nil, e
	}

	return ins, nil
}

/**
 * 链初始状态初始化，插入第一个区块之前的初始化操作
 */
func (b *ChainKernel) ChainStateIinitializeCall(stateinit func(interfaces.ChainStateOperation)) {
	b.insertLock.Lock()
	defer b.insertLock.Unlock()

	b.initcall = stateinit
}

/**
 * interfaces
 */
func (bc *ChainKernel) Start() error {
	// do nothing
	return nil
}

func (bc *ChainKernel) Close() error {
	bc.insertLock.Lock()
	defer bc.insertLock.Unlock()
	// close
	bc.stateImmutable.Close()
	bc.blockstore.Close()
	bc.stateCurrent.Destory()
	bc.stateImmutable.Destory()
	bc.stateCurrent = nil
	bc.stateImmutable = nil
	bc.blockstore = nil
	return nil
}

func (bc *ChainKernel) InsertBlock(newblk interfaces.Block, origin string) error {
	if origin != "" {
		newblk.SetOriginMark(origin)
	}
	_, _, e := bc.DiscoverNewBlockToInsert(newblk, origin)
	return e
}

func (b ChainKernel) StateRead() interfaces.ChainStateOperationRead {
	b.insertLock.RLock()
	defer b.insertLock.RUnlock()

	return b.stateCurrent
}

func (b ChainKernel) CurrentState() interfaces.ChainState {
	b.insertLock.RLock()
	defer b.insertLock.RUnlock()

	return b.stateCurrent
}

func (bc *ChainKernel) SubscribeValidatedBlockOnInsert(blockCh chan interfaces.Block) {
	bc.validatedBlockInsertFeed.Subscribe(blockCh)
}

func (bc *ChainKernel) SubscribeDiamondOnCreate(diamondCh chan *stores.DiamondSmelt) {
	bc.diamondCreateFeed.Subscribe(diamondCh)
}
