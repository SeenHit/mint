package blockchainv3

import (
	"fmt"
	"github.com/hacash/chain/blockstorev3"
	"github.com/hacash/core/blocks"
	"github.com/hacash/core/interfaces"
	"github.com/hacash/core/sys"
	"sync"
)

func updateDatabaseReturnBlockChain(ini *sys.Inicnf, olddatadir string, maxtarhei uint64, isclosenew bool) (*ChainKernel, error) {

	// 开始升级
	oldblockdatadir := olddatadir + "/blockstore"
	cnf1 := blockstorev3.NewEmptyBlockStoreConfig()
	cnf1.Datadir = oldblockdatadir
	oldblockDB, e0 := blockstorev3.NewBlockStore(cnf1)
	if e0 != nil {
		// 发生错误，返回
		return nil, fmt.Errorf("Check And Update Blockchain Database Version Error: %s", e0.Error())
	}
	defer oldblockDB.Close()

	// 建立新状态
	bccnf := NewChainKernelConfig(ini)
	chainCore, e1 := NewChainKernel(bccnf)
	if e1 != nil {
		return nil, fmt.Errorf("Check And Update Blockchain Database Version, NewBlockChain Error: %s", e1.Error())
		// 发生错误，返回
	}
	// 初始化
	chainCore.ChainStateIinitializeCall(setupHacashChainState)
	// 设定为数据库升级模式
	chainCore.CurrentState().SetDatabaseVersionRebuildMode(true)
	// 模式恢复
	defer func() {
		chainCore.CurrentState().SetDatabaseVersionRebuildMode(false) // 模式恢复
		// 外部决定是否关闭
		if isclosenew {
			chainCore.Close()
		}
	}()

	// 并行读取和写入
	updateDataCh := make(chan []byte, 50)
	updateBlockCh := make(chan interfaces.Block, 50)
	finishWait := sync.WaitGroup{}
	finishWait.Add(3)

	// 读取数据
	go func() {
		readblockhei := uint64(0)
		for {
			readblockhei++
			//fmt.Println("1")
			_, body, e := oldblockDB.ReadBlockBytesByHeight(readblockhei)
			if e != nil {
				fmt.Println("Check And Update Blockchain Database Version, ReadBlockBytesLengthByHeight Error:", e.Error())
				break // 发生错误，返回
			}
			if len(body) == 0 {
				break // 全部结束
			}
			// 写入数据
			updateDataCh <- body
			// 判断最高同步
			if maxtarhei > 0 && maxtarhei <= readblockhei {
				break // 完成全部
			}
		}
		// 读取完毕
		updateDataCh <- nil
		finishWait.Done()
	}()

	// 解析区块
	go func() {
		for {
			body := <-updateDataCh
			if body == nil {
				break // 完毕
			}
			//fmt.Println("3")
			// 解析区块
			blk, _, e2 := blocks.ParseBlock(body, 0)
			if e2 != nil {
				fmt.Println("Check And Update Blockchain Database Version, ParseBlock Error:", e2.Error())
				break // 发生错误，返回
			}
			// 写入数据
			updateBlockCh <- blk
		}
		// 读取完毕
		updateBlockCh <- nil
		finishWait.Done()
	}()

	// 写入区块数据
	go func() {
		readblockhei := uint64(1)
		for {
			blk := <-updateBlockCh
			if blk == nil {
				fmt.Printf("\b\b\b\b\b\b\b\b\b\b%10d", readblockhei)
				break // 完毕
			}

			//fmt.Println("4")
			// 插入区块（升级模式）
			e3 := chainCore.InsertBlock(blk, "")
			if e3 != nil {
				fmt.Println("Check And Update Blockchain Database Version, InsertBlock Error:", e3.Error())
				break // 发生错误，返回
			}
			//fmt.Println("5")
			// 打印
			if readblockhei%1000 == 0 {
				//fmt.Printf("%d", readblockhei)
				fmt.Printf("\b\b\b\b\b\b\b\b\b\b%10d", readblockhei)
			}
			//fmt.Println("6")
			// next block
			readblockhei++
		}
		// 插入结束
		finishWait.Done()
	}()

	finishWait.Wait()

	return chainCore, nil
}

// 检查升级数据库版本
func CheckAndUpdateBlockchainDatabaseVersion(ini *sys.Inicnf) error {
	curversion, compatible := ini.GetDatabaseVersion()
	_, has := ini.MustDataDirCheckVersion(curversion)
	if has {
		return nil // 当前版本已经存在，正常返回
	}
	// 需要升级，检查历史版本
	olddir := ""
	oldversion := curversion - 1
	for {
		if oldversion < compatible {
			// 已经低于最低可兼容版本了，表示区块出现了分叉，必须全部从网络重新同步
			return nil
		}
		olddir, has = ini.MustDataDirCheckVersion(oldversion)
		if has {
			break
		}
		oldversion--
	}

	// 依次读取区块，并插入新状态
	fmt.Printf("[Database] Upgrade blockchain database version v%d to v%d, block data is NOT resynchronized, Please wait and do not close the program...\n[Database] Checking block height:          0", oldversion, curversion)

	_, e := updateDatabaseReturnBlockChain(ini, olddir, 0, true)
	if e != nil {
		err := fmt.Errorf("Check And Update Blockchain Database Version, NewBlockChain Error: %s\n", e.Error())
		fmt.Println(err.Error())
		// 发生错误，返回
		return err
	}

	//fmt.Println("7", olddir)
	// 删除旧版本
	// defer os.RemoveAll(olddir)

	//fmt.Println("8")
	// 全部区块同步成功
	fmt.Printf(" all finished.\n[Database] version v%d => v%d upgrade successfully!\n", oldversion, curversion)

	return nil
}
