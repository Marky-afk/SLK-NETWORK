package main

import (
	"fmt"
	"image/color"
	"os"
	"os/exec"
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
	"github.com/slkproject/slk/race/manager"
	"github.com/slkproject/slk/network/p2p"
	"github.com/slkproject/slk/wallet"
)

var (
	bc         *chain.Blockchain
	mempool    *state.Mempool
	myWallet   *wallet.Wallet
	p2pNode    *p2p.Node
	walletPath = os.Getenv("HOME") + "/.slk/wallet.json"

	// UI labels updated from goroutines
	balanceLabel  *widget.Label
	trophiesLabel *widget.Label
	peersLabel    *widget.Label
	statusLabel   *widget.Label
	distLabel     *widget.Label
	powerLabel    *widget.Label
	tempLabel     *widget.Label
	timeLabel     *widget.Label
	tierLabel     *widget.Label
	vdfBar        *widget.ProgressBar
	vdfLabel      *widget.Label
	logBox        *widget.Label

	raceRunning   int32 // atomic
	stopRaceChan  = make(chan struct{}, 1)
	logLines      []string
	logMu         = make(chan struct{}, 1)
)

func main() {
	a := app.New()
	a.Settings().SetTheme(theme.DarkTheme())
	w := a.NewWindow("⛏️  SLK Mining Node")
	w.Resize(fyne.NewSize(600, 780))
	w.SetFixedSize(false)

	myWallet, _ = wallet.LoadOrCreate(walletPath)
	bc = chain.NewBlockchain()
	mempool = state.NewMempool()
	myWallet.SyncBalance(bc.UTXOSet.GetTotalBalance(myWallet.Address))

	// Start P2P
	go func() {
		var err error
		p2pNode, err = p2p.NewNode(30303, os.Getenv("HOME")+"/.slk")
		if err != nil {
			addLog("❌ P2P failed: " + err.Error())
			return
		}
		p2pNode.Start()
		addLog(fmt.Sprintf("🌐 P2P started — %d peers", p2pNode.PeerCount))
		// Trophy receive handler
		p2pNode.OnTrophy = func(t p2p.TrophyMsg) {
			if t.Winner == myWallet.Address {
				return
			}
			addLog(fmt.Sprintf("📡 Trophy #%d from %s", t.Height, t.Winner[:12]))
		}
	}()

	w.SetContent(buildUI(w))
	w.ShowAndRun()
}

func buildUI(w fyne.Window) fyne.CanvasObject {
	// ── HEADER ──
	title := canvas.NewText("⛏️  SLK MINING NODE", color.RGBA{R: 0, G: 200, B: 100, A: 255})
	title.TextSize = 22
	title.TextStyle = fyne.TextStyle{Bold: true}
	title.Alignment = fyne.TextAlignCenter

	// ── WALLET INFO ──
	balanceLabel = widget.NewLabel(fmt.Sprintf("💰 %.8f SLK", myWallet.Balance))
	balanceLabel.TextStyle = fyne.TextStyle{Bold: true}
	balanceLabel.Alignment = fyne.TextAlignCenter

	trophiesLabel = widget.NewLabel(fmt.Sprintf("🏆 Trophies: %d", bc.Height))
	trophiesLabel.Alignment = fyne.TextAlignCenter

	addrLabel := widget.NewLabel("📬 " + myWallet.Address)
	addrLabel.Alignment = fyne.TextAlignCenter
	addrLabel.Wrapping = fyne.TextWrapWord

	peersLabel = widget.NewLabel("🌍 Peers: connecting...")
	peersLabel.Alignment = fyne.TextAlignCenter

	// ── RACE STATS ──
	raceTitle := canvas.NewText("── LIVE RACE ──", color.RGBA{R: 255, G: 200, B: 0, A: 255})
	raceTitle.TextSize = 14
	raceTitle.TextStyle = fyne.TextStyle{Bold: true}
	raceTitle.Alignment = fyne.TextAlignCenter

	tierLabel = widget.NewLabel("Tier: —")
	tierLabel.Alignment = fyne.TextAlignCenter

	timeLabel = widget.NewLabel("⏱ Time: 0.0s")
	timeLabel.Alignment = fyne.TextAlignCenter

	distLabel = widget.NewLabel("📍 Distance Left: —")
	distLabel.Alignment = fyne.TextAlignCenter

	powerLabel = widget.NewLabel("⚡ Power: —")
	powerLabel.Alignment = fyne.TextAlignCenter

	tempLabel = widget.NewLabel("🌡 Temp: —")
	tempLabel.Alignment = fyne.TextAlignCenter

	// ── VDF ──
	vdfLabel = widget.NewLabel("🔐 VDF: idle")
	vdfLabel.Alignment = fyne.TextAlignCenter

	vdfBar = widget.NewProgressBar()
	vdfBar.Min = 0
	vdfBar.Max = 1
	vdfBar.SetValue(0)

	// ── STATUS ──
	statusLabel = widget.NewLabel("⏸ Press START to begin mining")
	statusLabel.Alignment = fyne.TextAlignCenter
	statusLabel.Wrapping = fyne.TextWrapWord

	// ── LOG ──
	logBox = widget.NewLabel("Starting up...")
	logBox.Wrapping = fyne.TextWrapWord

	logScroll := container.NewScroll(logBox)
	logScroll.SetMinSize(fyne.NewSize(560, 160))

	// ── BUTTONS ──
	startBtn := widget.NewButton("🏁 START MINING", nil)
	startBtn.Importance = widget.HighImportance

	stopBtn := widget.NewButton("⏹ STOP", nil)
	stopBtn.Importance = widget.DangerImportance
	stopBtn.Disable()

	startBtn.OnTapped = func() {
		if atomic.LoadInt32(&raceRunning) == 1 {
			return
		}
		startBtn.Disable()
		stopBtn.Enable()
		fyne.Do(func() {
			statusLabel.SetText("🏁 Mining started — racing...")
		})
		go runMiningLoop(w, startBtn, stopBtn)
	}

	stopBtn.OnTapped = func() {
		if atomic.LoadInt32(&raceRunning) == 1 {
			select {
			case stopRaceChan <- struct{}{}:
			default:
			}
		}
		manager.StopRace()
		exec.Command("pkill", "-9", "stress-ng").Run()
		atomic.StoreInt32(&raceRunning, 0)
		startBtn.Enable()
		stopBtn.Disable()
		fyne.Do(func() {
			statusLabel.SetText("⏸ Mining stopped")
			distLabel.SetText("📍 Distance Left: —")
			powerLabel.SetText("⚡ Power: —")
			tempLabel.SetText("🌡 Temp: —")
			vdfBar.SetValue(0)
			vdfLabel.SetText("🔐 VDF: idle")
		})
		addLog("⏹ Mining stopped by user")
	}

	// ── AUTO REFRESH peers + balance ──
	go func() {
		for {
			time.Sleep(5 * time.Second)
			myWallet.SyncBalance(bc.UTXOSet.GetTotalBalance(myWallet.Address))
			peers := 0
			if p2pNode != nil {
				peers = p2pNode.PeerCount
			}
			fyne.Do(func() {
				balanceLabel.SetText(fmt.Sprintf("💰 %.8f SLK", myWallet.Balance))
				trophiesLabel.SetText(fmt.Sprintf("🏆 Trophies: %d", bc.Height))
				peersLabel.SetText(fmt.Sprintf("🌍 Peers: %d", peers))
			})
		}
	}()

	// ── LAYOUT ──
	walletCard := container.NewVBox(
		balanceLabel,
		trophiesLabel,
		addrLabel,
		peersLabel,
	)

	raceCard := container.NewVBox(
		raceTitle,
		tierLabel,
		timeLabel,
		distLabel,
		powerLabel,
		tempLabel,
		widget.NewSeparator(),
		vdfLabel,
		vdfBar,
	)

	btnRow := container.NewGridWithColumns(2, startBtn, stopBtn)

	return container.NewVBox(
		container.NewCenter(title),
		widget.NewSeparator(),
		container.NewPadded(walletCard),
		widget.NewSeparator(),
		container.NewPadded(raceCard),
		widget.NewSeparator(),
		container.NewPadded(statusLabel),
		container.NewPadded(btnRow),
		widget.NewSeparator(),
		container.NewPadded(logScroll),
	)
}

func runMiningLoop(w fyne.Window, startBtn, stopBtn *widget.Button) {
	raceNum := bc.Height + 1
	for {
		// Check stop signal
		select {
		case <-stopRaceChan:
			return
		default:
		}

		atomic.StoreInt32(&raceRunning, 1)
		distance := bc.AdjustedDistance(1)
		addLog(fmt.Sprintf("🏁 Race #%d starting — %.0fm", raceNum, distance))
		fyne.Do(func() {
			statusLabel.SetText(fmt.Sprintf("🏁 Race #%d — GO!", raceNum))
			distLabel.SetText(fmt.Sprintf("📍 Distance: %.3fm", distance))
			vdfBar.SetValue(0)
			vdfLabel.SetText("🔐 VDF: waiting for race...")
		})

		err := manager.StartRace(0, distance)
		if err != nil {
			addLog("❌ Race start failed: " + err.Error())
			time.Sleep(3 * time.Second)
			continue
		}

		startTime := time.Now()
		finished := false

		for !finished {
			select {
			case <-stopRaceChan:
				manager.StopRace()
				exec.Command("pkill", "-9", "stress-ng").Run()
				atomic.StoreInt32(&raceRunning, 0)
				fyne.Do(func() {
					startBtn.Enable()
					stopBtn.Disable()
					statusLabel.SetText("⏸ Mining stopped")
				})
				return
			default:
			}

			state := manager.GetTelemetry()
			elapsed := time.Since(startTime).Seconds()

			goldT, silverT, _ := chain.CalculateTargetTime(distance)
			tierStr := "🥉 BRONZE"
			t := trophy.Bronze
			if elapsed <= goldT {
				tierStr = "🥇 GOLD"
				t = trophy.Gold
			} else if elapsed <= silverT {
				tierStr = "🥈 SILVER"
				t = trophy.Silver
			}

			fyne.Do(func() {
				tierLabel.SetText("Tier: " + tierStr)
				timeLabel.SetText(fmt.Sprintf("⏱ Time: %.1fs", elapsed))
				distLabel.SetText(fmt.Sprintf("📍 Distance Left: %.3fm", state.DistanceLeft))
				powerLabel.SetText(fmt.Sprintf("⚡ Power: %.1fW", state.CPUPowerWatts))
				tempLabel.SetText(fmt.Sprintf("🌡 Temp: %.0f°C", state.CPUTempCelsius))
			})

			if state.DistanceLeft < 0.001 || state.Status == manager.StatusFinished {
				finished = true
				manager.StopRace()
				exec.Command("pkill", "-9", "stress-ng").Run()
				addLog(fmt.Sprintf("🏆 Race #%d WON! %.2fs — %s", raceNum, elapsed, tierStr))
				fyne.Do(func() {
					statusLabel.SetText(fmt.Sprintf("🏆 Race #%d WON! Computing VDF...", raceNum))
					vdfLabel.SetText("🔐 VDF: computing proof...")
				})

				// VDF with animated bar
				seed := []byte(fmt.Sprintf("%s:%.0f:%.2f:%d", myWallet.Address, distance, elapsed, raceNum))
				vdfIter := uint64(distance * 1000)
				if vdfIter < 1000 { vdfIter = 1000 }
				if vdfIter > 10000 { vdfIter = 10000 }

				type vdfRes struct { proof *vdfmath.Proof; err error }
				vdfCh := make(chan vdfRes, 1)
				go func() {
					p, e := vdfmath.Prove(seed, vdfIter)
					vdfCh <- vdfRes{p, e}
				}()

				vdfStart := time.Now()
				minAnim := 1500 * time.Millisecond
				vdfDone := false
				var vdfResult vdfRes
				for !vdfDone || time.Since(vdfStart) < minAnim {
					select {
					case r := <-vdfCh:
						vdfDone = true
						vdfResult = r
						vdfCh <- r
					default:
					}
					pct := time.Since(vdfStart).Seconds() / minAnim.Seconds()
					if pct > 0.99 { pct = 0.99 }
					if vdfDone && time.Since(vdfStart) >= minAnim { pct = 1.0 }
					fyne.Do(func() { vdfBar.SetValue(pct) })
					time.Sleep(60 * time.Millisecond)
				}
				// Read final result
				vdfResult = <-vdfCh
				fyne.Do(func() {
					vdfBar.SetValue(1.0)
					if vdfResult.err == nil && vdfResult.proof != nil {
						vdfLabel.SetText("✅ VDF: " + vdfResult.proof.Output[:16] + "...")
					} else {
						vdfLabel.SetText("⚠️ VDF failed")
					}
				})

				// Add trophy
				newTrophy := bc.AddTrophy(myWallet.Address, distance, elapsed, t)
				if vdfResult.proof != nil {
					newTrophy.VDFProof = vdfResult.proof.Output
					newTrophy.VDFInput = vdfResult.proof.Input
					newTrophy.Hash = newTrophy.ComputeHash()
					if int(newTrophy.Header.Height) <= len(bc.Trophies) {
						bc.Trophies[newTrophy.Header.Height-1] = newTrophy
					}
				}

				utxoKey := fmt.Sprintf("trophy:%d:%x", bc.Height, newTrophy.Hash[:8])
				newUTXO := bc.UTXOSet.NewUTXOEntry(utxoKey, 0, newTrophy.Reward, myWallet.Address, uint64(bc.Height))
				bc.UTXOSet.AddUTXO(newUTXO)
				bc.UTXOSet.Save()
				myWallet.SyncBalance(bc.UTXOSet.GetTotalBalance(myWallet.Address))
				myWallet.Save(walletPath)
				bc.SaveChain()

				addLog(fmt.Sprintf("✅ Trophy #%d | +%.8f SLK | Balance: %.8f", raceNum, newTrophy.Reward, myWallet.Balance))

				fyne.Do(func() {
					balanceLabel.SetText(fmt.Sprintf("💰 %.8f SLK", myWallet.Balance))
					trophiesLabel.SetText(fmt.Sprintf("🏆 Trophies: %d", bc.Height))
					statusLabel.SetText(fmt.Sprintf("✅ Trophy #%d won! Next race in 3s...", raceNum))
				})

				// Broadcast
				if p2pNode != nil {
					go p2pNode.BroadcastTrophy(p2p.TrophyMsg{
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
				}

				time.Sleep(3 * time.Second)
				raceNum++
			}

			time.Sleep(200 * time.Millisecond)
		}
	}
}

func addLog(msg string) {
	ts := time.Now().Format("15:04:05")
	line := fmt.Sprintf("[%s] %s", ts, msg)
	logLines = append(logLines, line)
	if len(logLines) > 30 {
		logLines = logLines[len(logLines)-30:]
	}
	text := ""
	for i := len(logLines) - 1; i >= 0; i-- {
		text += logLines[i] + "\n"
	}
	fyne.Do(func() {
		if logBox != nil {
			logBox.SetText(text)
		}
	})
}
