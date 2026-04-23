package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"path/filepath"

	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"
	"github.com/slkproject/slk/core/chain"
	vdfmath "github.com/slkproject/slk/race/math"
	"github.com/slkproject/slk/core/state"
	"github.com/slkproject/slk/core/trophy"
	"github.com/slkproject/slk/network/p2p"
	"github.com/slkproject/slk/race/manager"
	"github.com/slkproject/slk/wallet"
)

var walletPath = os.Getenv("HOME") + "/.slk/wallet.json"
var myUsername = ""

var myWallet *wallet.Wallet
var bc *chain.Blockchain
var p2pNode *p2p.Node
var mempool *state.Mempool

// ONE global scanner and input channel
var globalScanner *bufio.Scanner
var inputChan = make(chan string, 10)

// Live racer tracking - updated from network broadcasts
type NetworkRacer struct {
	Address      string
	DistanceLeft float64
	Power        float64
	Temp         float64
	Status       string
	Username     string
	LastSeen     time.Time
}
var networkRacers = make(map[string]*NetworkRacer)
var racersMutex   sync.Mutex
var myRacerActive bool
var myRacerState  NetworkRacer

func main() {
	// ── ONE NODE PER MACHINE LOCK ──
	lockPath := filepath.Join(os.Getenv("HOME"), ".slk", "slkd.lock")
	os.MkdirAll(filepath.Dir(lockPath), 0755)
	lockFile, lockErr := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if lockErr != nil {
		fmt.Println("❌ Cannot create lock file:", lockErr)
		os.Exit(1)
	}
	lockErr = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if lockErr != nil {
		fmt.Println("╔══════════════════════════════════════════╗")
		fmt.Println("║   ❌ SLK NODE ALREADY RUNNING!           ║")
		fmt.Println("║   Only 1 node allowed per machine.       ║")
		fmt.Println("║   Close the other terminal first!        ║")
		fmt.Println("╚══════════════════════════════════════════╝")
		os.Exit(1)
	}
	// Release lock on exit
	defer func() {
		syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		lockFile.Close()
		os.Remove(lockPath)
	}()

	fmt.Println("╔══════════════════════════════════════════╗")
	fmt.Println("║     SLK PROOF-OF-RACE BLOCKCHAIN NODE    ║")
	fmt.Println("╚══════════════════════════════════════════╝")
	// ── AUTO PERFORMANCE MODE ──
	exec.Command("sudo", "cpufreq-set", "-g", "performance").Run()
	fmt.Println("⚡ CPU set to performance mode")

	var err error
	myWallet, err = wallet.LoadOrCreate(walletPath)
	if err != nil {
		fmt.Println("❌ Wallet error:", err)
		os.Exit(1)
	}

	// Load or create username
	usernamePath := os.Getenv("HOME") + "/.slk/username.txt"
	if data, err := os.ReadFile(usernamePath); err == nil && len(data) > 0 {
		myUsername = strings.TrimSpace(string(data))
	} else {
		fmt.Print("\n🏷️  Enter your node username (e.g. FANKLIN-MOZACK): ")
		globalScanner = bufio.NewScanner(os.Stdin)
		globalScanner.Scan()
		myUsername = strings.TrimSpace(globalScanner.Text())
		if myUsername == "" { myUsername = "ANON-RACER" }
		os.WriteFile(usernamePath, []byte(myUsername), 0644)
	}
	fmt.Printf("🏷️  Username: %s\n", myUsername)
	fmt.Printf("📬 Address:  %s\n", myWallet.Address)
	fmt.Printf("💰 Balance:  %.8f SLK\n", myWallet.Balance)

	bc = chain.NewBlockchain()
	mempool = state.NewMempool()

	// ── CRITICAL: Balance comes from UTXO only, never from wallet file ──
	realBalance := bc.UTXOSet.GetTotalBalance(myWallet.Address)
	myWallet.SyncBalance(realBalance)
	fmt.Printf("💰 Real Balance (from UTXO): %.8f SLK\n", myWallet.Balance)

	// ONE global input reader — runs forever
	globalScanner = bufio.NewScanner(os.Stdin)
	go func() {
		for globalScanner.Scan() {
			inputChan <- strings.TrimSpace(globalScanner.Text())
		}
	}()

	// Start P2P
	var p2pErr error
	p2pNode, p2pErr = p2p.NewNode(30303, os.Getenv("HOME")+"/.slk")
	if p2pErr != nil {
		fmt.Println("❌ P2P failed to start:", p2pErr)
		os.Exit(1)
	}

	// Track last peer count to detect new connections
	lastPeerCount := 0
	p2pNode.OnTrophy = func(t p2p.TrophyMsg) {
		// STEP 1: Reject if height is not next expected block
		if t.Height != bc.Height+1 {
			fmt.Printf("\n⚠️  REJECTED trophy from %s — bad height %d (expected %d)\n",
				t.Winner[:min(16, len(t.Winner))], t.Height, bc.Height+1)
			return
		}

		// STEP 2: Reject if PrevHash doesn't match our chain tip
		tip := bc.Trophies[len(bc.Trophies)-1]
		if fmt.Sprintf("%x", tip.Hash) != t.PrevHash {
			fmt.Printf("\n⚠️  REJECTED trophy from %s — prevHash mismatch\n",
				t.Winner[:min(16, len(t.Winner))])
			return
		}

		// STEP 3: Verify VDF proof — real computational work was done
		if t.VDFProof != "" && t.VDFInput != "" {
			vdfOk := vdfmath.Verify(&vdfmath.Proof{
				Input:      t.VDFInput,
				Output:     t.VDFProof,
				Iterations: uint64(t.Distance * 1000),
			})
			if !vdfOk {
				fmt.Printf("\n⚠️  REJECTED trophy from %s — VDF proof INVALID (FAKE RACE!)\n",
					t.Winner[:min(16, len(t.Winner))])
				return
			}
			fmt.Printf("\n🔐 VDF proof verified for trophy #%d\n", t.Height)
		}

		// STEP 4: Recompute hash and verify it matches what peer claims
		newT := trophy.NewTrophy(tip.Hash, t.Winner, t.Distance, t.Time, trophy.Tier(t.Tier), t.Height)
		if fmt.Sprintf("%x", newT.Hash) != t.Hash {
			fmt.Printf("\n⚠️  REJECTED trophy from %s — hash invalid (FAKE!)\n",
				t.Winner[:min(16, len(t.Winner))])
			return
		}

		// STEP 4: Valid — add to our chain
		bc.Trophies = append(bc.Trophies, newT)
		bc.Height = t.Height
		bc.TotalSupply -= newT.Reward
		fmt.Printf("\n✅ VALID trophy #%d from %s — added to chain\n",
			t.Height, t.Winner[:min(16, len(t.Winner))])
	}
	p2pNode.OnRacer = func(r p2p.RacerMsg) {
		racersMutex.Lock()
		if r.Status == "JOINED" {
			fmt.Printf("\n🏎️  NEW RACER joined: %s\n", r.Address[:min(16, len(r.Address))])
		}
		networkRacers[r.Address] = &NetworkRacer{
			Address:      r.Address,
			DistanceLeft: r.DistanceLeft,
			Power:        r.Power,
			Temp:         r.Temp,
			Status:       r.Status,
			Username:     r.Username,
			LastSeen:     time.Now(),
		}
		racersMutex.Unlock()
	}

	p2pNode.OnTx = func(tx p2p.TxMsg) {
		// Save to disk so receiver keeps TX even after restart
		wallet.SavePendingTransaction(wallet.Transaction{
			ID:         tx.ID,
			Type:       tx.Type,
			From:       tx.From,
			To:         tx.To,
			Amount:     tx.Amount,
			Timestamp:  tx.Timestamp,
			Signature:  tx.Signature,
			FromPubKey: tx.PubKey,
			Status:     "pending",
		})
		err := mempool.Add(&state.MempoolTx{
			ID:        tx.ID,
			From:      tx.From,
			To:        tx.To,
			Amount:    tx.Amount,
			Timestamp: tx.Timestamp,
			Signature: tx.Signature,
			PubKey:    tx.PubKey,
			Type:      tx.Type,
		})
		if err == nil {
			fmt.Printf("\n📥 Incoming TX: %.8f SLK from %s\n", tx.Amount, tx.From[:min(16, len(tx.From))])
			if tx.To == myWallet.Address {
				fmt.Printf("💰 You received %.8f SLK! Use option [6] to claim it.\n", tx.Amount)
				myWallet.Save(walletPath)
				fmt.Printf("💰 Balance updated: %.8f SLK\n", myWallet.Balance)
			}
		}
	}
p2pNode.Start()

	// ── CHAIN SYNC SETUP ──
	p2p.OnChainRequest = func(fromHeight uint64) p2p.ChainResponse {
		var trophies []p2p.TrophyMsg
		for _, t := range bc.Trophies {
			if t.Header.Height > fromHeight {
				trophies = append(trophies, p2p.TrophyMsg{
					Winner:   t.Winner,
					Distance: t.Distance,
					Time:     t.FinishTime,
					Tier:     int(t.Tier),
					Hash:     fmt.Sprintf("%x", t.Hash),
					PrevHash: fmt.Sprintf("%x", t.PrevHash),
					Height:   t.Header.Height,
				})
			}
		}
		return p2p.ChainResponse{Trophies: trophies, Height: bc.Height}
	}
	p2pNode.ServeChainSync()
	go func() {
		time.Sleep(5 * time.Second)
		resp, err := p2pNode.SyncWithBestPeer(bc.Height)
		if err != nil {
			return
		}
		// ── LONGEST CHAIN RULE — same as Bitcoin ──
		if resp.Height <= bc.Height {
			return // our chain is longer or equal, ignore
		}
		synced := 0
		for _, t := range resp.Trophies {
			if t.Height != bc.Height+1 {
				continue
			}
			// Verify VDF proof on every synced trophy
			if t.VDFProof != "" && t.VDFInput != "" {
				vdfOk := vdfmath.Verify(&vdfmath.Proof{
					Input:      t.VDFInput,
					Output:     t.VDFProof,
					Iterations: uint64(t.Distance * 1000),
				})
				if !vdfOk {
					fmt.Printf("\n🚨 SYNC rejected trophy #%d — fake VDF proof!\n", t.Height)
					break
				}
			}
			tip := bc.Trophies[len(bc.Trophies)-1]
			newT := trophy.NewTrophy(tip.Hash, t.Winner, t.Distance, t.Time, trophy.Tier(t.Tier), t.Height)
			bc.Trophies = append(bc.Trophies, newT)
			bc.Height = t.Height
			bc.TotalSupply -= newT.Reward
			synced++
		}
		if synced > 0 {
			// Sync balance after chain update
			myWallet.SyncBalance(bc.UTXOSet.GetTotalBalance(myWallet.Address))
			fmt.Printf("\n🔗 SYNCED %d trophies — chain height now %d\n", synced, bc.Height)
		}
	}()



	// ── WAIT FOR NETWORK BEFORE SHOWING MENU ──
	fmt.Println("\n⏳ Connecting to SLK network, please wait...")
	waitForNetwork()

	// Start peer monitor AFTER menu is shown
	go monitorPeers(&lastPeerCount)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\n\n🛑 Shutting down node...")
		manager.StopRace()
		myWallet.Save(walletPath)
		os.Exit(0)
	}()

	// Start HTTP API on port 8080
	go startAPIServer()

	showMenu()
}

func startAPIServer() {
	http.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"height":       bc.Height,
			"total_supply": 2000000000.0 - float64(bc.Height)*0.008,
			"peers":        p2pNode.PeerCount,
			"my_address":   myWallet.Address,
			"my_balance":   myWallet.Balance,
			"username":     myUsername,
		})
	})

	http.HandleFunc("/api/trophies", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		type TrophyJSON struct {
			Height   uint64  `json:"height"`
			Winner   string  `json:"winner"`
			Distance float64 `json:"distance"`
			Time     float64 `json:"time"`
			Reward   float64 `json:"reward"`
			Hash     string  `json:"hash"`
			Tier     string  `json:"tier"`
		}
		var trophies []TrophyJSON
		for _, t := range bc.Trophies {
			if t.Header.Height == 0 { continue }
			tierStr := "Gold"
			if t.Tier == 1 { tierStr = "Silver" }
			if t.Tier == 2 { tierStr = "Bronze" }
			trophies = append(trophies, TrophyJSON{
				Height:   t.Header.Height,
				Winner:   t.Winner,
				Distance: t.Distance,
				Time:     t.FinishTime,
				Reward:   t.Reward,
				Hash:     fmt.Sprintf("%x", t.Hash),
				Tier:     tierStr,
			})
		}
		if trophies == nil { trophies = []TrophyJSON{} }
		json.NewEncoder(w).Encode(trophies)
	})

	http.HandleFunc("/api/leaderboard", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		type Entry struct {
			Address string  `json:"address"`
			Trophies int    `json:"trophies"`
			Balance  float64 `json:"balance"`
		}
		counts := make(map[string]int)
		for _, t := range bc.Trophies {
			if t.Header.Height == 0 { continue }
			counts[t.Winner]++
		}
		var entries []Entry
		for addr, count := range counts {
			entries = append(entries, Entry{
				Address:  addr,
				Trophies: count,
				Balance:  float64(count) * 0.008,
			})
		}
		// Sort by trophies
		for i := 0; i < len(entries); i++ {
			for j := i+1; j < len(entries); j++ {
				if entries[j].Trophies > entries[i].Trophies {
					entries[i], entries[j] = entries[j], entries[i]
				}
			}
		}
		if entries == nil { entries = []Entry{} }
		json.NewEncoder(w).Encode(entries)
	})

	http.HandleFunc("/api/racers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		racersMutex.Lock()
		defer racersMutex.Unlock()
		type RacerJSON struct {
			Address      string  `json:"address"`
			DistanceLeft float64 `json:"distance_left"`
			Power        float64 `json:"power"`
			Temp         float64 `json:"temp"`
			Status       string  `json:"status"`
			Username     string  `json:"username"`
		}
		var racers []RacerJSON
		// Add yourself if currently racing
		if myRacerActive {
			racers = append(racers, RacerJSON{
				Address:      myRacerState.Address,
				DistanceLeft: myRacerState.DistanceLeft,
				Power:        myRacerState.Power,
				Temp:         myRacerState.Temp,
				Status:       myRacerState.Status,
				Username:     myUsername,
			})
		}
		for _, r := range networkRacers {
			racers = append(racers, RacerJSON{
				Address:      r.Address,
				DistanceLeft: r.DistanceLeft,
				Power:        r.Power,
				Temp:         r.Temp,
				Status:       r.Status,
				Username:     r.Username,
			})
		}
		if racers == nil { racers = []RacerJSON{} }
		json.NewEncoder(w).Encode(racers)
	})

	// API server started silently
	http.ListenAndServe(":8080", nil)
}

// waitForNetwork blocks until at least 1 peer connects
func waitForNetwork() {
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	dots := 0
	for {
		select {
		case <-ticker.C:
			if p2pNode.PeerCount > 0 {
				fmt.Printf("\r🌍 Connected! %d peers on the SLK network.\n", p2pNode.PeerCount)
				return
			}
			dots++
			fmt.Printf("\r⏳ Connecting to SLK network%s   ", strings.Repeat(".", dots%4))
		case <-timeout:
			fmt.Println("\n⚠️  Could not connect after 30s. Racing disabled until connected.")
			return
		}
	}
}

// monitorPeers only announces likely real SLK users (small increases)
func monitorPeers(lastCount *int) {
	ticker := time.NewTicker(15 * time.Second)
	for range ticker.C {
		current := p2pNode.PeerCount
		diff := current - *lastCount
		if diff >= 1 && diff <= 3 {
			fmt.Printf("\n🟢 %d new racer(s) joined the SLK network! Total peers: %d\n",
				diff, current)
			fmt.Print("\nChoose option: ")
		}
		*lastCount = current
	}
}

func showMenu() {
	for {
		fmt.Println()
		fmt.Println("╔══════════════════════════════════════════╗")
		myTrophies := 0
		for _, t := range bc.Trophies {
			if t.Winner == myWallet.Address {
				myTrophies++
			}
		}
		fmt.Printf("║  Trophies Won: %-4d | SLK: %.8f  ║\n", myTrophies, myWallet.Balance)
		fmt.Printf("║  Mempool: %-3d pending txs                       ║\n", mempool.Size())
		fmt.Println("╠══════════════════════════════════════════╣")
		fmt.Println("║  [1] Start Racing (auto-continues)       ║")
		fmt.Println("║  [2] Check Wallet                        ║")
		fmt.Println("║  [3] View Trophy Chain                   ║")
		fmt.Println("║  [4] P2P Network Status                  ║")
		fmt.Println("║  [5] Send Transaction                    ║")
		fmt.Println("║  [6] Check Incoming SLK                  ║")
		fmt.Println("║  [7] Exit                                ║")
		fmt.Println("╚══════════════════════════════════════════╝")
		fmt.Print("\nChoose option: ")

		choice := <-inputChan

		switch choice {
		case "1":
			// Block racing if offline
			if p2pNode == nil || p2pNode.PeerCount == 0 {
				fmt.Println("❌ Cannot race — not connected to network!")
				fmt.Println("   Waiting for network connection...")
				waitForNetwork()
			} else {
				startMining()
			}
		case "2":
			checkWallet()
		case "3":
			viewTrophies()
		case "4":
			p2pStatus()
		case "5":
			sendTransaction(bc)
		case "6":
			checkIncomingTransactions()
		case "7":
			myWallet.Save(walletPath)
			fmt.Println("👋 Goodbye!")
			os.Exit(0)
		default:
			fmt.Println("❌ Invalid option")
		}
	}
}

func startMining() {
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════╗")
	fmt.Println("║     ⛏️  SLK MINER STARTED                ║")
	fmt.Println("║  F=Full Power  S=Cool Down  Q=Stop       ║")
	fmt.Printf("║  🌍 Racing on network with %d peers       ║\n", p2pNode.PeerCount)
	fmt.Println("╚══════════════════════════════════════════╝")

	// Announce to network that we are racing
	if p2pNode != nil {
		p2pNode.BroadcastRacerPosition(p2p.RacerMsg{
			Address:      myWallet.Address,
			DistanceLeft: chain.CalculateDistance(activeRacerCount(), bc.Height),
			Power:        0,
			Temp:         0,
			Status:       "JOINED",
			Username:     myUsername,
		})
	}
	myRacerActive = true
	myRacerState = NetworkRacer{
		Address:  myWallet.Address,
		DistanceLeft: 0,
		Power:    0,
		Temp:     0,
		Status:   "RACING",
		LastSeen: time.Now(),
	}

	raceNum := bc.Height + 1

	for {
		distance := chain.CalculateDistance(activeRacerCount(), bc.Height)

		fmt.Println()
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Printf("  🏁 RACE #%d STARTING — Distance: %.0fm\n", raceNum, distance)
		fmt.Printf("  🌍 Peers watching: %d\n", p2pNode.PeerCount)
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

		err := manager.StartRace(0, distance)
		if err != nil {
			fmt.Println("❌ Failed to start race:", err)
			return
		}

		startTime := time.Now()
		finished := false
		stopped := false
		broadcastTick := 0

		// Input handler goroutine — reads Q/S/F instantly without blocking display
		cmdChan := make(chan string, 5)
		go func() {
			for {
				select {
				case input := <-inputChan:
					cmdChan <- strings.ToUpper(input)
				}
			}
		}()

		for !finished && !stopped {
			// Check commands instantly — non-blocking
			loop:
			for {
				select {
				case cmd := <-cmdChan:
					switch cmd {
					case "S":
						manager.SetThrottle(false)
						fmt.Println("\n❄️  COOLING DOWN... (press F to go full speed)")
					case "F":
						manager.SetThrottle(true)
						fmt.Println("\n🔥 FULL SPEED!")
					case "Q":
						fmt.Println("\n\n🛑 Mining stopped.")
						manager.StopRace()
						myWallet.Save(walletPath)
						stopped = true
						break loop
					}
				default:
					break loop
				}
			}
			if stopped { break }
			state := manager.GetTelemetry()
			myRacerState = NetworkRacer{
				Address:      myWallet.Address,
				DistanceLeft: state.DistanceLeft,
				Power:        state.CPUPowerWatts,
				Temp:         state.CPUTempCelsius,
				Status:       "RACING",
				LastSeen:     time.Now(),
			}
				elapsed := time.Since(startTime).Seconds()

				goldT, silverT, bronzeT := chain.CalculateTargetTime(distance)
				tierStr := "🥇 GOLD"
				t := trophy.Gold
				if elapsed > goldT {
					tierStr = "🥈 SILVER"
					t = trophy.Silver
				}
				if elapsed > silverT {
					tierStr = "🥉 BRONZE"
					t = trophy.Bronze
				}
				_ = bronzeT

				warning := ""
				if state.CPUTempCelsius >= 95 {
					warning = " 🚨 THROTTLING!"
					manager.SetThrottle(false)
				} else if state.CPUTempCelsius >= 90 {
					warning = " ⚠️  HOT! Press S"
				}

				// Build leaderboard
				racersMutex.Lock()
				// Remove stale racers (not seen in 30s)
				for addr, r := range networkRacers {
					if time.Since(r.LastSeen) > 30*time.Second {
						delete(networkRacers, addr)
					}
				}
				// Build sorted list
				type entry struct {
					addr string
					dist float64
					isMe bool
				}
				entries := []entry{{addr: myWallet.Address, dist: state.DistanceLeft, isMe: true}}
				for addr, r := range networkRacers {
					if r.Status == "RACING" || r.Status == "JOINED" {
						entries = append(entries, entry{addr: addr, dist: r.DistanceLeft, isMe: false})
					}
				}
				racersMutex.Unlock()

				// Sort by distance left (lowest = winning)
				for i := 0; i < len(entries); i++ {
					for j := i+1; j < len(entries); j++ {
						if entries[j].dist < entries[i].dist {
							entries[i], entries[j] = entries[j], entries[i]
						}
					}
				}

				myPos := 1
				for i, e := range entries {
					if e.isMe { myPos = i+1 }
				}

				// Clear screen and redraw dashboard in place
				fmt.Print("\033[2J\033[H")
				fmt.Println("╔══════════════════════════════════════════════════════════╗")
				fmt.Printf( "║  ⛏️  SLK RACE #%-3d  |  %-28s       ║\n", raceNum, tierStr)
				fmt.Println("╠══════════════════════════════════════════════════════════╣")
				fmt.Printf( "║  ⏱  Time:     %6.1fs                                    ║\n", elapsed)
				fmt.Printf( "║  🌍 Peers:    %-3d                                        ║\n", p2pNode.PeerCount)
				fmt.Printf( "║  🏆 Position: #%d of %-3d racers                           ║\n", myPos, len(entries))
				if warning != "" {
					fmt.Printf("║  %s%-50s║\n", warning, "")
				}
				fmt.Println("╠══════════════════════════════════════════════════════════╣")
				fmt.Println("║  POS  RACER              DIST LEFT     POWER    TEMP     ║")
				fmt.Println("╠══════════════════════════════════════════════════════════╣")
				for i, e := range entries {
					marker := " "
					name := e.addr[:min(14, len(e.addr))]
					if e.isMe {
						marker = "►"
						name = "YOU (" + e.addr[:min(8,len(e.addr))] + ")"
					}
					pow := func() float64 { if e.isMe { return state.CPUPowerWatts }; racersMutex.Lock(); defer racersMutex.Unlock(); if r, ok := networkRacers[e.addr]; ok { return r.Power }; return 0 }()
					tmp := func() float64 { if e.isMe { return state.CPUTempCelsius }; racersMutex.Lock(); defer racersMutex.Unlock(); if r, ok := networkRacers[e.addr]; ok { return r.Temp }; return 0 }()
					fmt.Printf("║ %s #%-2d  %-16s  %9.3fm    %5.1fW   %4.0f°C  ║\n",
						marker, i+1, name, e.dist, pow, tmp)
				}
				fmt.Println("╠══════════════════════════════════════════════════════════╣")
				fmt.Println("║  [S] Cool Down   [F] Full Speed   [Q] Stop Racing        ║")
				fmt.Println("╚══════════════════════════════════════════════════════════╝")

				// Broadcast position every 5 ticks (~2.5 seconds)
				broadcastTick++
				if broadcastTick%5 == 0 && p2pNode != nil {
					p2pNode.BroadcastRacerPosition(p2p.RacerMsg{
						Address:      myWallet.Address,
						DistanceLeft: state.DistanceLeft,
						Power:        state.CPUPowerWatts,
						Temp:         state.CPUTempCelsius,
						Status:       "RACING",
						Username:     myUsername,
					})
				}

				if state.Status == manager.StatusFinished && !finished {
					finished = true
					manager.StopRace()

					fmt.Println()
					fmt.Println("╔══════════════════════════════════════════╗")
					fmt.Printf("║  🏆 RACE #%d WON!                         ║\n", raceNum)
					fmt.Println("╠══════════════════════════════════════════╣")
					fmt.Printf("║  Time:    %.2fs\n", elapsed)
					fmt.Printf("║  Tier:    %s\n", tierStr)
					fmt.Printf("║  Reward:  %.8f SLK\n", trophy.BlockReward)
					fmt.Println("╚══════════════════════════════════════════╝")

					// ── REAL VDF PROOF ──
					fmt.Println("\n🔐 Computing VDF proof (cryptographic race certificate)...")
					seed := []byte(fmt.Sprintf("%s:%.0f:%.2f:%d", myWallet.Address, distance, elapsed, raceNum))
					vdfIterations := uint64(distance * 1000)
					if vdfIterations < 10000 { vdfIterations = 10000 }
					if vdfIterations > 500000 { vdfIterations = 500000 }
					proof, vdfErr := vdfmath.Prove(seed, vdfIterations)
					if vdfErr != nil {
						fmt.Println("⚠️  VDF failed:", vdfErr)
					} else {
						fmt.Printf("✅ VDF Proof: %s...\n", proof.Output[:16])
					}

					newTrophy := bc.AddTrophy(myWallet.Address, distance, elapsed, t)

					// Attach VDF proof to trophy
					if vdfErr == nil {
						newTrophy.VDFProof = proof.Output
						newTrophy.VDFInput = proof.Input
					}

					fmt.Printf("✅ Trophy #%d added to chain!\n", raceNum)
					fmt.Printf("[TROPHY #%d]\n", raceNum)
					fmt.Printf("  Winner:   %s\n", newTrophy.Winner)
					fmt.Printf("  Distance: %.9f m\n", newTrophy.Distance)
					fmt.Printf("  Time:     %.2fs\n", newTrophy.FinishTime)
					fmt.Printf("  Hash:     %x\n", newTrophy.Hash)
					fmt.Printf("  PrevHash: %x\n", newTrophy.PrevHash)

					// Broadcast trophy to real network
					if p2pNode != nil {
						err := p2pNode.BroadcastTrophy(p2p.TrophyMsg{
							Winner:   myWallet.Address,
							Distance: distance,
							Time:     elapsed,
							Tier:     int(t),
							Hash:     fmt.Sprintf("%x", newTrophy.Hash),
							PrevHash: fmt.Sprintf("%x", newTrophy.PrevHash),
							Height:   newTrophy.Header.Height,
							VDFProof: newTrophy.VDFProof,
							VDFInput: newTrophy.VDFInput,
						})
						if err != nil {
							fmt.Printf("⚠️  Broadcast failed: %v\n", err)
						} else {
							fmt.Printf("📡 Trophy broadcast to %d peers on the network!\n", p2pNode.PeerCount)
					// Desktop popup notification
					go func(rn uint64, peers int) {
						msg := fmt.Sprintf("Race #%d won! +0.00800000 SLK | VDF verified | %d peers notified", rn, peers)
						switch runtime.GOOS {
						case "linux":
							exec.Command("notify-send", "🏆 SLK TROPHY WON!", msg, "--urgency=critical").Run()
						case "darwin":
							exec.Command("osascript", "-e",
								fmt.Sprintf("display notification \"%s\" with title \"SLK TROPHY WON!\"", msg)).Run()
						case "windows":
							exec.Command("powershell", "-Command",
								fmt.Sprintf("[System.Windows.Forms.MessageBox]::Show('%s','SLK Trophy Won!')", msg)).Run()
						}
					}(raceNum, p2pNode.PeerCount)
					
						}
					}

					myWallet.Balance += newTrophy.Reward
					// Confirm all pending mempool txs in this block
					pending := mempool.GetAll()
					ids := make([]string, len(pending))
					for i, tx := range pending { ids[i] = tx.ID }
					mempool.ConfirmBlock(ids)
					myWallet.Save(walletPath)

					fmt.Printf("💰 Total SLK Remaining: %.3f\n", 2000000000.0-float64(bc.Height)*trophy.BlockReward)
					fmt.Printf("💰 Balance: %.8f SLK | Trophies: %d\n", myWallet.Balance, bc.Height)
					fmt.Println("⏳ Next race starting in 3 seconds...")
					time.Sleep(3 * time.Second)
					// Drain any stale keypresses before next race
					for len(inputChan) > 0 { <-inputChan }
					for len(cmdChan) > 0 { <-cmdChan }
					raceNum++
				}

			time.Sleep(500 * time.Millisecond)
		}

		if stopped {
			myRacerActive = false
			// Drain ALL stale keypresses — don't forward anything to menu
			for len(cmdChan) > 0 { <-cmdChan }
			for len(inputChan) > 0 { <-inputChan }
			return
		}
	}
}

func activeRacerCount() int {
	racersMutex.Lock()
	defer racersMutex.Unlock()
	count := 1 // count ourselves
	for _, r := range networkRacers {
		if time.Since(r.LastSeen) < 30*time.Second &&
			(r.Status == "RACING" || r.Status == "JOINED") {
			count++
		}
	}
	return count
}

func checkWallet() {
	myWallet.Print()
	fmt.Println("  [S] Show Private Key   [H] Hide   [ENTER] Back")
	fmt.Print("\nChoice: ")
	shown := false
	for {
		input := strings.ToUpper(<-inputChan)
		switch input {
		case "S":
			privHex := myWallet.PrivKeyHex()
			fmt.Println("\n  ⚠️  PRIVATE KEY — DO NOT SHARE!")
			fmt.Printf("  %s\n", privHex[:64])
			fmt.Printf("  %s\n", privHex[64:])
			fmt.Print("  [H] Hide   [ENTER] Back\n\nChoice: ")
			shown = true
		case "H":
			if shown {
				myWallet.Print()
				fmt.Print("  [S] Show   [H] Hide   [ENTER] Back\n\nChoice: ")
				shown = false
			}
		default:
			bc.UTXOSet.PrintUTXOs(myWallet.Address)
			return
		}
	}
}

func viewTrophies() {
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════╗")
	fmt.Println("║            🏆 TROPHY CHAIN               ║")
	fmt.Println("╚══════════════════════════════════════════╝")
	for i := 1; i < len(bc.Trophies); i++ {
		fmt.Println(bc.Trophies[i].String())
		fmt.Println("────────────────────────────────────────────")
	}
	if bc.Height == 0 {
		fmt.Println("  No trophies yet — start racing!")
	}
	fmt.Printf("Chain valid: %v\n", bc.IsValid())
}

func p2pStatus() {
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                  🌐 P2P NETWORK STATUS                          ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════════╣")
	if p2pNode != nil {
		status := "🟢 ONLINE"
		if p2pNode.PeerCount == 0 {
			status = "🔴 OFFLINE"
		}
		fmt.Printf("║  Status:  %-54s║\n", status)
		fmt.Printf("║  Peers:   %-3d connected                                          ║\n", p2pNode.PeerCount)
		fmt.Printf("║  Port:    30303                                                  ║\n")
		fmt.Printf("║  PeerID:  %s  ║\n", p2pNode.PeerID()[:40])
		fmt.Println("║  Network: slk-proof-of-race-mainnet-v1                           ║")
		fmt.Println("╠══════════════════════════════════════════════════════════════════╣")
		fmt.Println("║  Share this to let others join YOUR blockchain:                  ║")
		fmt.Printf("║  /ip4/41.90.70.28/tcp/30303/p2p/%s\n", p2pNode.PeerID())
	} else {
		fmt.Println("║  Status:  🔴 OFFLINE                                             ║")
	}
	fmt.Println("╚══════════════════════════════════════════════════════════════════╝")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func sendTransaction(bc *chain.Blockchain) {
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════╗")
	fmt.Println("║              💸 SEND SLK TRANSACTION             ║")
	fmt.Println("╠══════════════════════════════════════════════════╣")
	fmt.Println("║  [1] Standard Transaction                        ║")
	fmt.Println("║      → Receiver gets SLK instantly               ║")
	fmt.Println("║  [2] Independent Transaction                     ║")
	fmt.Println("║      → Receiver needs SECRET CODE from you       ║")
	fmt.Println("║  [3] Back                                        ║")
	fmt.Println("╚══════════════════════════════════════════════════╝")
	fmt.Print("\nChoose type: ")

	// using global inputChan
	// scan via inputChan
	choice := strings.TrimSpace(<-inputChan)

	switch choice {
	case "1":
		doStandardTransaction(bc)
	case "2":
		doIndependentTransaction(bc)
	case "3":
		return
	default:
		fmt.Println("❌ Invalid option")
	}
}

func doStandardTransaction(bc *chain.Blockchain) {
	// using global inputChan

retry:
	fmt.Println("\n╔══════════════════════════════════════════════════╗")
	fmt.Println("║         📤 STANDARD TRANSACTION                  ║")
	fmt.Println("╚══════════════════════════════════════════════════╝")

	// Get receiver address
	fmt.Print("\n📬 Enter RECEIVER wallet address (SLK-xxxx-xxxx-xxxx-xxxx): ")
	// scan via inputChan
	receiver := strings.TrimSpace(<-inputChan)

	// Validate address format
	receiver = strings.TrimSpace(receiver)
	if !strings.HasPrefix(receiver, "SLK-") || len(receiver) < 15 {
		fmt.Println("❌ INVALID wallet address! Must start with SLK- and be valid format!")
		fmt.Println("   Try again...")
		goto retry
	}
	if receiver == myWallet.Address {
		fmt.Println("❌ Cannot send to yourself!")
		goto retry
	}

	// Get amount
	fmt.Print("💰 Enter amount to send (your balance: ", fmt.Sprintf("%.8f", myWallet.Balance), " SLK): ")
	// scan via inputChan
	var amount float64
	_, err := fmt.Sscanf(<-inputChan, "%f", &amount)
	if err != nil || amount <= 0 {
		fmt.Println("❌ Invalid amount!")
		goto retry
	}

	// ── REAL BALANCE CHECK from UTXO — cannot be faked ──
	realBalance := bc.UTXOSet.GetTotalBalance(myWallet.Address)
	myWallet.SyncBalance(realBalance)
	if amount > realBalance {
		fmt.Printf("❌ TRANSACTION DENIED! Insufficient balance!\n")
		fmt.Printf("   Your balance: %.8f SLK\n", myWallet.Balance)
		fmt.Printf("   Attempted:    %.8f SLK\n", amount)
		fmt.Println("   Transaction rejected by network!")
		return
	}

	// Get private key confirmation
	fmt.Print("\n🔑 Enter your PRIVATE KEY to authorize (64-byte hex): ")
	// scan via inputChan
	privKeyHex := strings.TrimSpace(<-inputChan)

	privKey, err := hex.DecodeString(privKeyHex)
	if err != nil || len(privKey) != 64 {
		fmt.Println("❌ INVALID private key! Must be 64-byte hex!")
		return
	}

	// Verify private key matches wallet
	if hex.EncodeToString(myWallet.PrivateKey) != privKeyHex {
		fmt.Println("❌ PRIVATE KEY MISMATCH! Transaction DENIED!")
		fmt.Println("   Network has rejected this transaction.")
		return
	}

	// First confirmation
	fmt.Println("\n╔══════════════════════════════════════════════════╗")
	fmt.Println("║              ⚠️  CONFIRM TRANSACTION              ║")
	fmt.Println("╠══════════════════════════════════════════════════╣")
	fmt.Printf( "║  From:   %s\n", myWallet.Address)
	fmt.Printf( "║  To:     %s\n", receiver)
	fmt.Printf( "║  Amount: %.8f SLK\n", amount)
	fmt.Println("║  Type:   Standard (instant delivery)            ║")
	fmt.Println("╠══════════════════════════════════════════════════╣")
	fmt.Println("║  Are you sure? [Y] Yes  [N] No                  ║")
	fmt.Println("╚══════════════════════════════════════════════════╝")
	fmt.Print("\nChoice: ")
	// scan via inputChan
	if strings.ToUpper(strings.TrimSpace(<-inputChan)) != "Y" {
		fmt.Println("❌ Transaction cancelled. Starting over...")
		goto retry
	}

	// Second warning
	fmt.Println("\n╔══════════════════════════════════════════════════╗")
	fmt.Println("║              🚨 FINAL WARNING 🚨                  ║")
	fmt.Println("╠══════════════════════════════════════════════════╣")
	fmt.Println("║  SLK transactions are IRREVERSIBLE!              ║")
	fmt.Println("║  Once sent, coins CANNOT be recovered!           ║")
	fmt.Println("║  Double-check the receiver address NOW!          ║")
	fmt.Println("╠══════════════════════════════════════════════════╣")
	fmt.Printf( "║  SENDING: %.8f SLK\n", amount)
	fmt.Printf( "║  TO:      %s\n", receiver)
	fmt.Println("╠══════════════════════════════════════════════════╣")
	fmt.Println("║  Type CONFIRM to proceed or CANCEL to abort:    ║")
	fmt.Println("╚══════════════════════════════════════════════════╝")
	fmt.Print("\n> ")
	// scan via inputChan
	if strings.ToUpper(strings.TrimSpace(<-inputChan)) != "CONFIRM" {
		fmt.Println("✅ Transaction aborted safely.")
		return
	}

	// Build and sign transaction
	ts := time.Now().Unix()
	tx := wallet.Transaction{
		ID:        wallet.GenerateTxID(myWallet.Address, receiver, amount, ts),
		Type:      wallet.TxStandard,
		From:      myWallet.Address,
		To:        receiver,
		Amount:    amount,
		Timestamp: ts,
		Status:    "pending",
	}

	err = wallet.SignTransaction(&tx, myWallet)
	if err != nil {
		fmt.Println("❌ Signing failed:", err)
		return
	}

	// Verify signature BEFORE rotating key
	if !wallet.VerifyTransactionSignature(&tx) {
		fmt.Println("❌ Signature verification failed! Transaction DENIED!")
		return
	}

	// Rotate key AFTER successful verification
	myWallet.RotatePrivateKey()
	myWallet.Save(walletPath)

	// Deduct from sender
	myWallet.Balance -= amount

	// Add to mempool
	mempool.Add(&state.MempoolTx{
		ID:        tx.ID,
		From:      tx.From,
		To:        tx.To,
		Amount:    tx.Amount,
		Timestamp: tx.Timestamp,
		Signature: tx.Signature,
		PubKey:    tx.FromPubKey,
		Type:      tx.Type,
	})

	// Broadcast to all peers
	if p2pNode != nil {
		p2pNode.BroadcastTx(p2p.TxMsg{
			ID:        tx.ID,
			From:      tx.From,
			To:        tx.To,
			Amount:    tx.Amount,
			Timestamp: tx.Timestamp,
			Signature: tx.Signature,
			PubKey:    tx.FromPubKey,
			Type:      tx.Type,
		})
		fmt.Printf("📡 TX broadcast to %d peers!\n", p2pNode.PeerCount)
	}

	// Save transaction
	tx.Status = "confirmed"
	wallet.SaveConfirmedTransaction(tx)
	wallet.SavePendingTransaction(tx)

	fmt.Println("\n╔══════════════════════════════════════════════════╗")
	fmt.Println("║           ✅ TRANSACTION CONFIRMED!               ║")
	fmt.Println("╠══════════════════════════════════════════════════╣")
	fmt.Printf( "║  TX ID:  %s\n", tx.ID)
	fmt.Printf( "║  From:   %s\n", tx.From)
	fmt.Printf( "║  To:     %s\n", tx.To)
	fmt.Printf( "║  Amount: %.8f SLK\n", tx.Amount)
	fmt.Printf( "║  Signed: ✅ Ed25519 libsodium\n")
	fmt.Printf( "║  Verified: ✅ Network accepted\n")
	fmt.Printf( "║  New Balance: %.8f SLK\n", myWallet.Balance)
	fmt.Println("║  🔄 Your private key has been rotated!           ║")
	fmt.Println("╚══════════════════════════════════════════════════╝")
}

func doIndependentTransaction(bc *chain.Blockchain) {
	// using global inputChan

retry2:
	fmt.Println("\n╔══════════════════════════════════════════════════╗")
	fmt.Println("║         🔐 INDEPENDENT TRANSACTION               ║")
	fmt.Println("║  Receiver needs SECRET CODE from you to claim    ║")
	fmt.Println("╚══════════════════════════════════════════════════╝")

	fmt.Print("\n📬 Enter RECEIVER wallet address: ")
	// scan via inputChan
	receiver := strings.TrimSpace(<-inputChan)

	if !strings.HasPrefix(receiver, "SLK-") || len(receiver) < 20 {
		fmt.Println("❌ INVALID wallet address!")
		goto retry2
	}
	if receiver == myWallet.Address {
		fmt.Println("❌ Cannot send to yourself!")
		goto retry2
	}

	{
		fmt.Print("💰 Enter amount (your balance: ", fmt.Sprintf("%.8f", myWallet.Balance), " SLK): ")
		// scan via inputChan
		var amount float64
		_, err := fmt.Sscanf(<-inputChan, "%f", &amount)
		if err != nil || amount <= 0 {
			fmt.Println("❌ Invalid amount!")
			goto retry2
		}

		if amount > myWallet.Balance {
			fmt.Printf("❌ TRANSACTION DENIED! Insufficient balance: %.8f SLK\n", myWallet.Balance)
			return
		}

		fmt.Print("\n🔑 Enter your PRIVATE KEY to authorize: ")
		// scan via inputChan
		privKeyHex := strings.TrimSpace(<-inputChan)

		privKey, err := hex.DecodeString(privKeyHex)
		if err != nil || len(privKey) != 64 {
			fmt.Println("❌ INVALID private key!")
			return
		}

		if hex.EncodeToString(myWallet.PrivateKey) != privKeyHex {
			fmt.Println("❌ PRIVATE KEY MISMATCH! Transaction DENIED!")
			return
		}

		// First confirmation
		fmt.Println("\n╔══════════════════════════════════════════════════╗")
		fmt.Println("║              ⚠️  CONFIRM TRANSACTION              ║")
		fmt.Println("╠══════════════════════════════════════════════════╣")
		fmt.Printf( "║  From:   %s\n", myWallet.Address)
		fmt.Printf( "║  To:     %s\n", receiver)
		fmt.Printf( "║  Amount: %.8f SLK\n", amount)
		fmt.Println("║  Type:   Independent (receiver needs code)      ║")
		fmt.Println("╠══════════════════════════════════════════════════╣")
		fmt.Println("║  [Y] Yes  [N] No                                ║")
		fmt.Println("╚══════════════════════════════════════════════════╝")
		fmt.Print("\nChoice: ")
		// scan via inputChan
		if strings.ToUpper(strings.TrimSpace(<-inputChan)) != "Y" {
			fmt.Println("❌ Cancelled. Starting over...")
			goto retry2
		}

		// Final warning
		fmt.Println("\n╔══════════════════════════════════════════════════╗")
		fmt.Println("║              🚨 FINAL WARNING 🚨                  ║")
		fmt.Println("╠══════════════════════════════════════════════════╣")
		fmt.Println("║  You MUST share the SECRET CODE with receiver!  ║")
		fmt.Println("║  If receiver fails 3 times → SLK returns to YOU ║")
		fmt.Println("║  Transaction is IRREVERSIBLE once confirmed!     ║")
		fmt.Println("╠══════════════════════════════════════════════════╣")
		fmt.Println("║  Type CONFIRM to proceed:                       ║")
		fmt.Println("╚══════════════════════════════════════════════════╝")
		fmt.Print("\n> ")
		// scan via inputChan
		if strings.ToUpper(strings.TrimSpace(<-inputChan)) != "CONFIRM" {
			fmt.Println("✅ Transaction aborted.")
			return
		}

		// Generate secret code
		secretCode := wallet.GenerateSecretCode()

		// Build and sign
		ts := time.Now().Unix()
		tx := wallet.Transaction{
			ID:         wallet.GenerateTxID(myWallet.Address, receiver, amount, ts),
			Type:       wallet.TxIndependent,
			From:       myWallet.Address,
			To:         receiver,
			Amount:     amount,
			Timestamp:  ts,
			Status:     "pending",
			SecretCode: secretCode,
		}

		err = wallet.SignTransaction(&tx, myWallet)
		if err != nil {
			fmt.Println("❌ Signing failed:", err)
			return
		}

		if !wallet.VerifyTransactionSignature(&tx) {
			fmt.Println("❌ Signature verification failed!")
			return
		}

		// Lock the amount
		myWallet.Balance -= amount
		wallet.SavePendingTransaction(tx)

		fmt.Println("\n╔══════════════════════════════════════════════════╗")
		fmt.Println("║        ✅ INDEPENDENT TX CREATED!                ║")
		fmt.Println("╠══════════════════════════════════════════════════╣")
		fmt.Printf( "║  TX ID:      %s\n", tx.ID)
		fmt.Printf( "║  Amount:     %.8f SLK (LOCKED)\n", amount)
		fmt.Printf( "║  Receiver:   %s\n", receiver)
		fmt.Println("╠══════════════════════════════════════════════════╣")
		fmt.Println("║  🔐 SECRET CODE (SHARE ONLY WITH RECEIVER!):    ║")
		fmt.Printf( "║  >>> %s <<<\n", secretCode)
		fmt.Println("╠══════════════════════════════════════════════════╣")
		fmt.Println("║  ⚠️  NEVER share this code with anyone else!     ║")
		fmt.Println("║  Receiver has 3 attempts to enter it correctly  ║")
		fmt.Println("║  After 3 failures → SLK returns to your wallet  ║")
		fmt.Println("║  🔄 Your private key has been rotated!           ║")
		fmt.Println("╚══════════════════════════════════════════════════╝")
	}
}

func checkIncomingTransactions() {
	// using global inputChan

	fmt.Println("\n╔══════════════════════════════════════════════════╗")
	fmt.Println("║         📥 CHECK INCOMING TRANSACTIONS           ║")
	fmt.Println("╚══════════════════════════════════════════════════╝")

	// Auto-use your own wallet address — no need to type it
	address := myWallet.Address
	fmt.Printf("\n📬 Checking incoming SLK for: %s\n", address)

	pending := wallet.GetPendingForAddress(address)
	if len(pending) == 0 {
		fmt.Println("\n📭 No incoming transactions found for this address.")
		return
	}

	fmt.Printf("\n📬 Found %d incoming transaction(s):\n\n", len(pending))
	for i, tx := range pending {
		txType := "Standard"
		if tx.Type == wallet.TxIndependent {
			txType = "Independent (needs SECRET CODE)"
		}
		fmt.Printf("  [%d] TX ID: %s\n", i+1, tx.ID)
		fmt.Printf("      From:   %s\n", tx.From)
		fmt.Printf("      Amount: %.8f SLK\n", tx.Amount)
		fmt.Printf("      Type:   %s\n", txType)
		fmt.Printf("      Time:   %s\n\n", time.Unix(tx.Timestamp, 0).Format("2006-01-02 15:04:05"))
	}

	fmt.Print("Enter TX number to claim (or 0 to cancel): ")
	// scan via inputChan
	var txNum int
	fmt.Sscanf(<-inputChan, "%d", &txNum)

	if txNum == 0 || txNum > len(pending) {
		fmt.Println("Cancelled.")
		return
	}

	selectedTx := pending[txNum-1]

	// Verify with public key first
	if !wallet.VerifyTransactionSignature(&selectedTx) {
		fmt.Println("❌ Transaction signature INVALID! Possible fraud detected!")
		return
	}
	fmt.Println("✅ Transaction signature verified with sender's public key!")

	// Standard tx - signature already verified, just claim it
	if selectedTx.Type == wallet.TxStandard {
		myWallet.Balance += selectedTx.Amount
		wallet.UpdatePendingTransaction(selectedTx.ID, "claimed")
		wallet.SaveConfirmedTransaction(selectedTx)
		mempool.Remove(selectedTx.ID)
		myWallet.Save(walletPath)

		fmt.Println("\n╔══════════════════════════════════════════════════╗")
		fmt.Println("║           ✅ SLK CLAIMED SUCCESSFULLY!            ║")
		fmt.Println("╠══════════════════════════════════════════════════╣")
		fmt.Printf( "║  Received: %.8f SLK\n", selectedTx.Amount)
		fmt.Printf( "║  From:     %s\n", selectedTx.From)
		fmt.Printf( "║  New Balance: %.8f SLK\n", myWallet.Balance)
		fmt.Println("║  Verified: ✅ Ed25519 signature confirmed        ║")
		fmt.Println("╚══════════════════════════════════════════════════╝")

	} else {
		// Independent tx - needs secret code
		fmt.Println("\n🔐 This is an INDEPENDENT transaction!")
		fmt.Println("   You need the SECRET CODE from the sender.")
		fmt.Println("   You have 3 attempts. After 3 failures SLK returns to sender!")

		for attempt := 1; attempt <= 3; attempt++ {
			fmt.Printf("\n🔑 Enter SECRET CODE (attempt %d/3): ", attempt)
			// scan via inputChan
			code := strings.ToUpper(strings.TrimSpace(<-inputChan))

			if code == selectedTx.SecretCode {
				// Enter private key
				fmt.Print("🔑 Enter YOUR private key to finalize: ")
				// scan via inputChan
				privKeyHex := strings.TrimSpace(<-inputChan)

				privKey, err := hex.DecodeString(privKeyHex)
				if err != nil || len(privKey) != 64 {
					fmt.Println("❌ Invalid private key!")
					return
				}

				if hex.EncodeToString(myWallet.PrivateKey) != privKeyHex {
					fmt.Println("❌ Private key mismatch!")
					return
				}

				myWallet.Balance += selectedTx.Amount
				wallet.UpdatePendingTransaction(selectedTx.ID, "claimed")
				wallet.SaveConfirmedTransaction(selectedTx)
				mempool.Remove(selectedTx.ID)
				myWallet.Save(walletPath)

				fmt.Println("\n╔══════════════════════════════════════════════════╗")
				fmt.Println("║        ✅ INDEPENDENT TX CLAIMED!                ║")
				fmt.Println("╠══════════════════════════════════════════════════╣")
				fmt.Printf( "║  Received:    %.8f SLK\n", selectedTx.Amount)
				fmt.Printf( "║  From:        %s\n", selectedTx.From)
				fmt.Printf( "║  New Balance: %.8f SLK\n", myWallet.Balance)
				fmt.Println("║  ✅ Secret code verified!                        ║")
				fmt.Println("║  ✅ Ed25519 signature confirmed!                  ║")
				fmt.Println("╚══════════════════════════════════════════════════╝")
				return
			}

			attempts := wallet.IncrementAttempts(selectedTx.ID)
			remaining := 3 - attempts
			if remaining <= 0 {
				fmt.Println("\n╔══════════════════════════════════════════════════╗")
				fmt.Println("║        ❌ 3 FAILED ATTEMPTS!                      ║")
				fmt.Println("╠══════════════════════════════════════════════════╣")
				fmt.Println("║  Transaction DENIED by network!                 ║")
				fmt.Printf( "║  %.8f SLK returned to sender!\n", selectedTx.Amount)
				fmt.Println("╚══════════════════════════════════════════════════╝")
				wallet.UpdatePendingTransaction(selectedTx.ID, "returned")
				return
			}
			fmt.Printf("❌ Wrong code! %d attempt(s) remaining!\n", remaining)
		}
	}
}
