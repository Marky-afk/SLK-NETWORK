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
	uiLogLines  []string
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
	a := app.New()
	a.Settings().SetTheme(theme.DarkTheme())
	w := a.NewWindow("SLK Mining Node")
	w.Resize(fyne.NewSize(580, 800))

	myWallet, _ = wallet.LoadOrCreate(walletPath)
	bc = chain.NewBlockchain()
	mempool = state.NewMempool()
	myWallet.SyncBalance(bc.UTXOSet.GetTotalBalance(myWallet.Address))

	setUI(func() {
		uiBalance  = myWallet.Balance
		uiTrophies = int(bc.Height)
		uiStatus   = "Press START MINING to begin"
		uiVDFText  = "idle"
		uiTier     = "—"
	})

	// Start P2P
	go func() {
		var err error
		p2pNode, err = p2p.NewNode(30303, os.Getenv("HOME")+"/.slk")
		if err != nil { addLog("❌ P2P: " + err.Error()); return }
		p2pNode.Start()
		addLog(fmt.Sprintf("🌐 P2P ready — %d peers", p2pNode.PeerCount))
		p2pNode.OnTrophy = func(t p2p.TrophyMsg) {
			if t.Winner != myWallet.Address {
				addLog(fmt.Sprintf("📡 Trophy #%d from %s", t.Height, t.Winner[:12]))
			}
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

	balLbl   := widget.NewLabel("")
	trophLbl  := widget.NewLabel("")
	addrLbl   := widget.NewLabel("📬 " + myWallet.Address)
	addrLbl.Wrapping = fyne.TextWrapWord
	addrLbl.Alignment = fyne.TextAlignCenter
	peersLbl  := widget.NewLabel("")

	raceHdr := canvas.NewText("── LIVE RACE ──", yellow)
	raceHdr.TextSize = 13
	raceHdr.TextStyle = fyne.TextStyle{Bold: true}
	raceHdr.Alignment = fyne.TextAlignCenter

	tierLbl   := widget.NewLabel("")
	timeLbl   := widget.NewLabel("")
	distLbl   := widget.NewLabel("")
	powerLbl  := widget.NewLabel("")
	tempLbl   := widget.NewLabel("")
	statusLbl := widget.NewLabel("")
	statusLbl.Wrapping = fyne.TextWrapWord
	statusLbl.Alignment = fyne.TextAlignCenter

	vdfLbl  := widget.NewLabel("")
	vdfBar  := widget.NewProgressBar()
	vdfBar.Min, vdfBar.Max = 0, 1

	logLbl := widget.NewLabel("")
	logLbl.Wrapping = fyne.TextWrapWord
	logScroll := container.NewScroll(logLbl)
	logScroll.SetMinSize(fyne.NewSize(540, 180))

	startBtn := widget.NewButton("🏁  START MINING", nil)
	startBtn.Importance = widget.HighImportance
	stopBtn  := widget.NewButton("⏹  STOP", nil)
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
				balLbl.SetText(fmt.Sprintf("💰  %.8f SLK", bal))
				trophLbl.SetText(fmt.Sprintf("🏆  Trophies: %d", troph))
				peersLbl.SetText(fmt.Sprintf("🌍  Peers: %d", peers))
				tierLbl.SetText("Tier: " + tier)
				timeLbl.SetText(fmt.Sprintf("⏱  Time: %.1fs", t))
				distLbl.SetText(fmt.Sprintf("📍  Distance Left: %.3fm", dist))
				powerLbl.SetText(fmt.Sprintf("⚡  Power: %.1fW", pwr))
				tempLbl.SetText(fmt.Sprintf("🌡  Temp: %.0f°C", tmp))
				statusLbl.SetText(status)
				vdfBar.SetValue(vdfPct)
				vdfLbl.SetText("🔐 VDF: " + vdfTxt)
				logLbl.SetText(log)
			})
		}
	}()

	// peer count ticker
	go func() {
		for range time.Tick(5 * time.Second) {
			if p2pNode != nil {
				setUI(func() { uiPeers = p2pNode.PeerCount })
			}
			myWallet.SyncBalance(bc.UTXOSet.GetTotalBalance(myWallet.Address))
			setUI(func() { uiBalance = myWallet.Balance; uiTrophies = int(bc.Height) })
		}
	}()

	return container.NewVBox(
		container.NewCenter(title),
		widget.NewSeparator(),
		container.NewPadded(container.NewVBox(
			balLbl, trophLbl, addrLbl, peersLbl,
		)),
		widget.NewSeparator(),
		container.NewPadded(container.NewVBox(
			raceHdr, tierLbl, timeLbl, distLbl, powerLbl, tempLbl,
			widget.NewSeparator(),
			vdfLbl, vdfBar,
		)),
		widget.NewSeparator(),
		container.NewPadded(statusLbl),
		container.NewPadded(container.NewGridWithColumns(2, startBtn, stopBtn)),
		widget.NewSeparator(),
		container.NewPadded(logScroll),
	)
}

func runMining(startBtn, stopBtn *widget.Button) {
	atomic.StoreInt32(&raceRunning, 1)
	raceNum := bc.Height + 1

	for {
		select {
		case <-stopMining:
			atomic.StoreInt32(&raceRunning, 0)
			return
		default:
		}

		distance := bc.AdjustedDistance(1)
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

			setUI(func() {
				uiTier  = tierStr
				uiTime  = elapsed
				uiDist  = st.DistanceLeft
				uiPower = st.CPUPowerWatts
				uiTemp  = st.CPUTempCelsius
			})

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
					uiBalance  = myWallet.Balance
					uiTrophies = int(bc.Height)
					uiStatus   = fmt.Sprintf("✅ Trophy #%d! +%.8f SLK — next in 3s...", raceNum, newT.Reward)
				})
				addLog(fmt.Sprintf("💰 Balance: %.8f SLK | Trophies: %d", myWallet.Balance, bc.Height))

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
