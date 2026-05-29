package main

import (
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/slkproject/slk/core/chain"
	"github.com/slkproject/slk/core/state"
	"github.com/slkproject/slk/core/trophy"
	vdfmath "github.com/slkproject/slk/race/math"
	"github.com/slkproject/slk/network/p2p"
	"github.com/slkproject/slk/race/manager"
	"github.com/slkproject/slk/wallet"
)

// ── shared state updated by mining goroutine, read by UI ticker ──
var (
	bc       *chain.Blockchain
	mempool  *state.Mempool
	myWallet *wallet.Wallet
	p2pNode  *p2p.Node
	walletPath = os.Getenv("HOME") + "/.slk/wallet.json"

	raceRunning  int32
	stopMining   = make(chan struct{}, 1)
	networkRacers   = map[string]p2p.RacerMsg{}
	racersMu        sync.Mutex
	peerWonRace     = make(chan struct{}, 1)

	uiMu        sync.Mutex
	uiBalance   float64
	uiTrophies  int
	uiPeers     int
	uiTier      string
	uiTime      float64
	uiDist      float64
	uiPower     float64
	uiTemp      float64
	uiStatus    string
	uiVDFPct    float64
	uiVDFText   string
	uiLogLines   []string
	uiDifficulty float64
	uiRacers       []string
	uiPeerList     []string
	uiHashRate      string
	uiAccepted      int
	uiRejected      int
	uiMiningTime    float64
	uiSessionEarned float64
)

func setUI(fn func()) { uiMu.Lock(); fn(); uiMu.Unlock() }

func addLog(msg string) {
	ts := time.Now().Format("15:04:05")
	uiMu.Lock()
	uiLogLines = append([]string{fmt.Sprintf("[%s] %s", ts, msg)}, uiLogLines...)
	if len(uiLogLines) > 40 { uiLogLines = uiLogLines[:40] }
	uiMu.Unlock()
}

func main() {
	a := app.NewWithID("io.slk.gui")
	a.Settings().SetTheme(theme.DarkTheme())
	w := a.NewWindow("SLK Mining Node")
	w.Resize(fyne.NewSize(580, 800))

	// Enforce slkd-first: wallet must exist before slkgui can run
	if _, err := os.Stat(walletPath); os.IsNotExist(err) {
		w.SetContent(container.NewCenter(container.NewVBox(
			widget.NewLabel("❌ No wallet found!"),
			widget.NewLabel("You must run ./build/slkd first to create your wallet and join the network."),
			widget.NewLabel("Once slkd is running, restart slkgui."),
			widget.NewButton("Exit", func() { a.Quit() }),
		)))
		w.ShowAndRun()
		return
	}
	myWallet, _ = wallet.LoadOrCreate(walletPath)
	bc = chain.NewBlockchain()
	mempool = state.NewMempool()
	myWallet.SyncBalance(bc.UTXOSet.GetTotalBalance(myWallet.Address))

	initDist := bc.AdjustedDistance(1)
	setUI(func() {
		uiBalance    = myWallet.Balance
		uiTrophies   = int(bc.Height)
		uiStatus     = "Press START MINING to begin"
		uiVDFText    = "idle"
		uiTier       = "—"
		uiDifficulty = initDist
	})

	// Start P2P — use separate dir so node_key doesn't conflict with slkbank
	go func() {
		var err error
		guiDir := os.Getenv("HOME") + "/.slk/gui"
		os.MkdirAll(guiDir, 0755)
		p2pNode, err = p2p.NewNode(30303, guiDir)
		if err != nil { addLog("❌ P2P: " + err.Error()); return }
		p2pNode.Start()
		// Wait for bootstrap connections to establish
		for i := 0; i < 12; i++ {
			time.Sleep(3 * time.Second)
			peerList := p2pNode.GetPeers()
			setUI(func() {
				uiPeers    = p2pNode.PeerCount
				uiPeerList = peerList
			})
			if p2pNode.PeerCount > 0 {
				addLog(fmt.Sprintf("🌐 P2P ready — %d peers", p2pNode.PeerCount))
				break
			}
		}
		if p2pNode.PeerCount == 0 {
			addLog("⚠️ No peers yet — still discovering...")
		}
		p2pNode.OnTrophy = func(t p2p.TrophyMsg) {
			if t.Winner != myWallet.Address {
				addLog(fmt.Sprintf("📡 Trophy #%d from %s", t.Height, t.Winner[:12]))
			}
		}
		p2pNode.OnRacer = func(r p2p.RacerMsg) {
			racersMu.Lock()
			if r.Status == "STOPPED" || r.Status == "FINISHED" {
				delete(networkRacers, r.Address)
			} else {
				networkRacers[r.Address] = r
			}
			list := make([]string, 0, len(networkRacers))
			for _, v := range networkRacers {
				name := v.Address
				if len(name) > 16 { name = name[:16] + "..." }
				if v.Username != "" { name = v.Username }
				list = append(list, fmt.Sprintf("🏃 %s  dist:%.2fm  ⚡%.0fW  %s", name, v.DistanceLeft, v.Power, v.Status))
			}
			racersMu.Unlock()
			setUI(func() { uiRacers = list })
		}
	}()

	w.SetContent(buildUI(w))
	w.ShowAndRun()
}

func buildUI(w fyne.Window) fyne.CanvasObject {
	green  := color.RGBA{R:0,   G:220, B:100, A:255}
	yellow := color.RGBA{R:255, G:200, B:0,   A:255}

	title := canvas.NewText("⛏  SLK MINING NODE", green)
	title.TextSize = 24
	title.TextStyle = fyne.TextStyle{Bold: true}
	title.Alignment = fyne.TextAlignCenter

	// ── Wallet / Stats bar ──
	balLbl  := widget.NewLabel("")
	balLbl.TextStyle = fyne.TextStyle{Bold: true}
	trophLbl := widget.NewLabel("")
	trophLbl.TextStyle = fyne.TextStyle{Bold: true}
	addrLbl  := widget.NewLabel("📬 " + myWallet.Address)
	addrLbl.Wrapping  = fyne.TextWrapWord
	addrLbl.Alignment = fyne.TextAlignCenter
	peersLbl := widget.NewLabel("")
	peersLbl.TextStyle = fyne.TextStyle{Bold: true}

	// ── Mining stats (Bitcoin-style) ──
	hashRateLbl     := widget.NewLabel("")
	hashRateLbl.TextStyle = fyne.TextStyle{Bold: true}
	acceptedLbl     := widget.NewLabel("")
	acceptedLbl.TextStyle = fyne.TextStyle{Bold: true}
	rejectedLbl     := widget.NewLabel("")
	sessionEarnLbl  := widget.NewLabel("")
	sessionEarnLbl.TextStyle = fyne.TextStyle{Bold: true}
	miningTimeLbl   := widget.NewLabel("")

	// ── Race panel ──
	raceHdr := canvas.NewText("── LIVE RACE ──", yellow)
	raceHdr.TextSize  = 13
	raceHdr.TextStyle = fyne.TextStyle{Bold: true}
	raceHdr.Alignment = fyne.TextAlignCenter

	tierLbl   := widget.NewLabel("")
	tierLbl.TextStyle = fyne.TextStyle{Bold: true}
	timeLbl   := widget.NewLabel("")
	distLbl   := widget.NewLabel("")
	powerLbl  := widget.NewLabel("")
	tempLbl   := widget.NewLabel("")
	diffLbl   := widget.NewLabel("")
	statusLbl := widget.NewLabel("")
	statusLbl.Wrapping  = fyne.TextWrapWord
	statusLbl.Alignment = fyne.TextAlignCenter
	statusLbl.TextStyle = fyne.TextStyle{Bold: true}

	vdfLbl := widget.NewLabel("")
	vdfBar := widget.NewProgressBar()
	vdfBar.Min, vdfBar.Max = 0, 1

	// ── Competitors / Peers ──
	racersHdr := canvas.NewText("── LIVE COMPETITORS ──", yellow)
	racersHdr.TextSize  = 12
	racersHdr.TextStyle = fyne.TextStyle{Bold: true}
	racersHdr.Alignment = fyne.TextAlignCenter
	racersLbl := widget.NewLabel("")
	racersLbl.Wrapping = fyne.TextWrapWord

	peersHdr := canvas.NewText("── CONNECTED PEERS ──", yellow)
	peersHdr.TextSize  = 12
	peersHdr.TextStyle = fyne.TextStyle{Bold: true}
	peersHdr.Alignment = fyne.TextAlignCenter
	peersListLbl := widget.NewLabel("")
	peersListLbl.Wrapping = fyne.TextWrapWord

	logLbl := widget.NewLabel("")
	logLbl.Wrapping = fyne.TextWrapWord
	logScroll := container.NewScroll(logLbl)
	logScroll.SetMinSize(fyne.NewSize(540, 150))

	// ── START / STOP buttons (large, prominent) ──
	startBtn := widget.NewButton("⛏️  START MINING", nil)
	startBtn.Importance = widget.HighImportance
	stopBtn  := widget.NewButton("🛑  STOP MINING", nil)
	stopBtn.Importance = widget.DangerImportance
	stopBtn.Disable()

	startBtn.OnTapped = func() {
		if atomic.LoadInt32(&raceRunning) == 1 { return }
		startBtn.Disable()
		stopBtn.Enable()
		go runMining(startBtn, stopBtn)
	}
	stopBtn.OnTapped = func() {
		select { case stopMining <- struct{}{}: default: }
		manager.StopRace()
		exec.Command("pkill", "-9", "stress-ng").Run()
		atomic.StoreInt32(&raceRunning, 0)
		startBtn.Enable()
		stopBtn.Disable()
		setUI(func() { uiStatus = "⏸ Stopped" })
		addLog("⏹ Mining stopped")
	}

	// ── UI refresh ticker — only place that touches widgets ──
	go func() {
		for range time.Tick(300 * time.Millisecond) {
			uiMu.Lock()
			bal     := uiBalance
			troph   := uiTrophies
			peers   := uiPeers
			tier    := uiTier
			t       := uiTime
			dist    := uiDist
			pwr     := uiPower
			tmp     := uiTemp
			status  := uiStatus
			vdfPct  := uiVDFPct
			vdfTxt  := uiVDFText
			lines   := make([]string, len(uiLogLines))
			copy(lines, uiLogLines)
			uiMu.Unlock()

			log := ""
			for _, l := range lines { log += l + "\n" }

			fyne.Do(func() {
				balLbl.SetText(fmt.Sprintf("💰  Balance: %.8f SLK", bal))
				trophLbl.SetText(fmt.Sprintf("🏆  Trophies: %d", troph))
				peersLbl.SetText(fmt.Sprintf("🌍  Peers: %d", peers))
				hashRateLbl.SetText("⚡ Hash Rate:   " + func() string { if uiHashRate == "" { return "0.00 H/s" }; return uiHashRate }())
				acceptedLbl.SetText(fmt.Sprintf("✅ Accepted:   %d", uiAccepted))
				rejectedLbl.SetText(fmt.Sprintf("❌ Rejected:   %d", uiRejected))
				sessionEarnLbl.SetText(fmt.Sprintf("💎 Session Earned: +%.8f SLK", uiSessionEarned))
				miningTimeLbl.SetText(fmt.Sprintf("⏳ Mining Time: %.0fs", uiMiningTime))
				diffLbl.SetText(fmt.Sprintf("🎯 Difficulty: %.1fm", uiDifficulty))
				tierLbl.SetText("🏅 Tier:  " + tier)
				timeLbl.SetText(fmt.Sprintf("⏱  Race Time: %.1fs", t))
				distLbl.SetText(fmt.Sprintf("📍 Distance Left: %.3fm", dist))
				powerLbl.SetText(fmt.Sprintf("⚡ CPU Power: %.1fW", pwr))
				tempLbl.SetText(fmt.Sprintf("🌡  CPU Temp: %.0f°C", tmp))
				statusLbl.SetText(status)
				vdfBar.SetValue(vdfPct)
				vdfLbl.SetText("🔐 VDF Proof: " + vdfTxt)
				logLbl.SetText(log)
				peerTxt := ""; for _, p := range uiPeerList { peerTxt += "🔗 " + p + "\n" }
				racerTxt := ""; for _, r := range uiRacers { racerTxt += r + "\n" }
				if peerTxt != "" { peersListLbl.SetText(peerTxt) }
				if racerTxt != "" { racersLbl.SetText(racerTxt) }
			})
		}
	}()

	// peer count ticker
	go func() {
		for range time.Tick(3 * time.Second) {
			if p2pNode != nil {
				peerList := p2pNode.GetPeers()
				setUI(func() {
					uiPeers    = p2pNode.PeerCount
					uiPeerList = peerList
				})
			}
			dist := bc.AdjustedDistance(activeRacerCount())
			myWallet.SyncBalance(bc.UTXOSet.GetTotalBalance(myWallet.Address))
			setUI(func() {
				uiBalance    = myWallet.Balance
				uiTrophies   = int(bc.Height)
				uiDifficulty = dist
			})
		}
	}()

	racersScroll := container.NewScroll(racersLbl)
	racersScroll.SetMinSize(fyne.NewSize(540, 70))
	peersScroll := container.NewScroll(peersListLbl)
	peersScroll.SetMinSize(fyne.NewSize(540, 70))

	// ── Bitcoin-style mining stats box ──
	miningStatsHdr := canvas.NewText("── MINING STATS ──", yellow)
	miningStatsHdr.TextSize  = 13
	miningStatsHdr.TextStyle = fyne.TextStyle{Bold: true}
	miningStatsHdr.Alignment = fyne.TextAlignCenter

	statsLeft  := container.NewVBox(hashRateLbl, acceptedLbl, rejectedLbl)
	statsRight := container.NewVBox(sessionEarnLbl, miningTimeLbl, diffLbl)
	statsGrid  := container.NewGridWithColumns(2, statsLeft, statsRight)

	// ── Wallet info box ──
	walletHdr := canvas.NewText("── WALLET ──", yellow)
	walletHdr.TextSize  = 13
	walletHdr.TextStyle = fyne.TextStyle{Bold: true}
	walletHdr.Alignment = fyne.TextAlignCenter

	return container.NewVBox(
		container.NewCenter(title),
		widget.NewSeparator(),

		// Wallet block
		container.NewPadded(container.NewVBox(
			walletHdr,
			container.NewCenter(addrLbl),
			container.NewGridWithColumns(3, balLbl, trophLbl, peersLbl),
		)),
		widget.NewSeparator(),

		// START / STOP buttons — big and prominent
		container.NewPadded(container.NewGridWithColumns(2, startBtn, stopBtn)),
		container.NewCenter(statusLbl),
		widget.NewSeparator(),

		// Mining stats (Bitcoin-style)
		container.NewPadded(container.NewVBox(
			miningStatsHdr,
			statsGrid,
		)),
		widget.NewSeparator(),

		// Live race panel
		container.NewPadded(container.NewVBox(
			raceHdr,
			container.NewGridWithColumns(2,
				container.NewVBox(tierLbl, timeLbl, distLbl),
				container.NewVBox(powerLbl, tempLbl),
			),
			widget.NewSeparator(),
			vdfLbl, vdfBar,
		)),
		widget.NewSeparator(),

		// Competitors
		container.NewPadded(container.NewVBox(racersHdr, racersScroll)),
		widget.NewSeparator(),

		// Peers
		container.NewPadded(container.NewVBox(peersHdr, peersScroll)),
		widget.NewSeparator(),

		// Activity log
		container.NewPadded(logScroll),
	)
}

func activeRacerCount() int {
	racersMu.Lock()
	defer racersMu.Unlock()
	return 1 + len(networkRacers)
}

func runMining(startBtn, stopBtn *widget.Button) {
	atomic.StoreInt32(&raceRunning, 1)
	raceNum     := bc.Height + 1
	sessionStart := time.Now()
	setUI(func() {
		uiAccepted      = 0
		uiRejected      = 0
		uiSessionEarned = 0
		uiMiningTime    = 0
		uiHashRate      = "calculating..."
		uiStatus        = "⛏️  Mining started..."
	})

	for {
		select {
		case <-stopMining:
			atomic.StoreInt32(&raceRunning, 0)
			return
		default:
		}

		distance := bc.AdjustedDistance(activeRacerCount())
		addLog(fmt.Sprintf("🏁 Race #%d — %.0fm", raceNum, distance))
		setUI(func() {
			uiStatus = fmt.Sprintf("🏁 Race #%d — GO!", raceNum)
			uiDist   = distance
			uiVDFPct = 0
			uiVDFText = "waiting..."
			uiTier   = "—"
			uiTime   = 0
		})

		if err := manager.StartRace(0, distance); err != nil {
			addLog("❌ " + err.Error())
			time.Sleep(2 * time.Second)
			continue
		}

		startTime := time.Now()
		finished  := false

		for !finished {
			select {
			case <-stopMining:
				manager.StopRace()
				exec.Command("pkill", "-9", "stress-ng").Run()
				atomic.StoreInt32(&raceRunning, 0)
				fyne.Do(func() { startBtn.Enable(); stopBtn.Disable() })
				return
			default:
			}

			st      := manager.GetTelemetry()
			elapsed := time.Since(startTime).Seconds()
			goldT, silverT, _ := chain.CalculateTargetTime(distance)
			tier := trophy.Bronze; tierStr := "🥉 BRONZE"
			if elapsed <= goldT   { tier = trophy.Gold;   tierStr = "🥇 GOLD" }
			if elapsed <= silverT { tier = trophy.Silver; tierStr = "🥈 SILVER" }
			if elapsed > goldT && elapsed <= silverT { tier = trophy.Silver; tierStr = "🥈 SILVER" }

			racePct := 0.0
			if distance > 0 { racePct = 1.0 - (st.DistanceLeft / distance) }
			if racePct < 0 { racePct = 0 }
			if racePct > 1 { racePct = 1 }
			setUI(func() {
				uiTier    = tierStr
				uiTime    = elapsed
				uiDist    = st.DistanceLeft
				uiPower   = st.CPUPowerWatts
				uiTemp    = st.CPUTempCelsius
				uiVDFPct  = racePct
				uiVDFText = fmt.Sprintf("Racing... %.0f%%", racePct*100)
			})
			// Update session mining time and hashrate
			sessionElapsed := time.Since(sessionStart).Seconds()
			hashRate := ""
			if elapsed > 0 {
				hr := distance / elapsed
				if hr >= 1000 { hashRate = fmt.Sprintf("%.2f KH/s", hr/1000) } else { hashRate = fmt.Sprintf("%.2f H/s", hr) }
			}
			setUI(func() { uiMiningTime = sessionElapsed; uiHashRate = hashRate })

			if p2pNode != nil {
				go p2pNode.BroadcastRacerPosition(p2p.RacerMsg{
					Address: myWallet.Address, DistanceLeft: st.DistanceLeft,
					Power: st.CPUPowerWatts, Temp: st.CPUTempCelsius,
					Status: "RACING", Username: "",
				})
			}

			if st.DistanceLeft < 0.001 || st.Status == manager.StatusFinished {
				finished = true
				manager.StopRace()
				exec.Command("pkill", "-9", "stress-ng").Run()
				addLog(fmt.Sprintf("🏆 Race #%d WON! %.2fs %s", raceNum, elapsed, tierStr))
				setUI(func() { uiStatus = fmt.Sprintf("🏆 Race #%d WON! Running VDF...", raceNum) })

				// VDF
				seed    := []byte(fmt.Sprintf("%s:%.0f:%.2f:%d", myWallet.Address, distance, elapsed, raceNum))
				vdfIter := uint64(distance * 1000)
				if vdfIter < 1000  { vdfIter = 1000 }
				if vdfIter > 10000 { vdfIter = 10000 }

				type vR struct{ p *vdfmath.Proof; e error }
				ch := make(chan vR, 1)
				go func() { p, e := vdfmath.Prove(seed, vdfIter); ch <- vR{p, e} }()

				vStart  := time.Now()
				minAnim := 1500 * time.Millisecond
				vDone   := false
				var vRes vR
				for !vDone || time.Since(vStart) < minAnim {
					select {
					case r := <-ch:
						vDone = true; vRes = r; ch <- r
					default:
					}
					pct := time.Since(vStart).Seconds() / minAnim.Seconds()
					if pct > 1 { pct = 1 }
					setUI(func() { uiVDFPct = pct; uiVDFText = fmt.Sprintf("%.0f%%", pct*100) })
					time.Sleep(60 * time.Millisecond)
				}
				vRes = <-ch
				setUI(func() { uiVDFPct = 1.0 })
				if vRes.e == nil && vRes.p != nil {
					setUI(func() { uiVDFText = "✅ " + vRes.p.Output[:16] + "..." })
					addLog("✅ VDF: " + vRes.p.Output[:16])
				} else {
					setUI(func() { uiVDFText = "⚠️ failed" })
				}

				// Save trophy
				newT := bc.AddTrophy(myWallet.Address, distance, elapsed, tier)
				if vRes.p != nil {
					newT.VDFProof = vRes.p.Output
					newT.VDFInput = vRes.p.Input
					newT.Hash = newT.ComputeHash()
					if int(newT.Header.Height) <= len(bc.Trophies) {
						bc.Trophies[newT.Header.Height-1] = newT
					}
				}
				utxoKey := fmt.Sprintf("trophy:%d:%x", bc.Height, newT.Hash[:8])
				newUTXO := bc.UTXOSet.NewUTXOEntry(utxoKey, 0, newT.Reward, myWallet.Address, uint64(bc.Height))
				bc.UTXOSet.AddUTXO(newUTXO)
				bc.UTXOSet.Save()
				myWallet.SyncBalance(bc.UTXOSet.GetTotalBalance(myWallet.Address))
				myWallet.Save(walletPath)
				bc.SaveChain()

				setUI(func() {
					uiBalance        = myWallet.Balance
					uiTrophies       = int(bc.Height)
					uiAccepted++
					uiSessionEarned += newT.Reward
					uiMiningTime     = time.Since(sessionStart).Seconds()
					uiStatus         = fmt.Sprintf("✅ Trophy #%d! +%.8f SLK — next in 3s...", raceNum, newT.Reward)
				})
				addLog(fmt.Sprintf("💰 Balance: %.8f SLK | Trophies: %d | Session: +%.8f SLK", myWallet.Balance, bc.Height, newT.Reward))

				if p2pNode != nil {
					go p2pNode.BroadcastTrophy(p2p.TrophyMsg{
						Winner: myWallet.Address, Distance: distance, Time: elapsed,
						Tier: int(tier), Hash: fmt.Sprintf("%x", newT.Hash),
						PrevHash: fmt.Sprintf("%x", newT.PrevHash),
						Height: newT.Header.Height, VDFProof: newT.VDFProof, VDFInput: newT.VDFInput,
					})
				}

				time.Sleep(3 * time.Second)
				raceNum++
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
}
