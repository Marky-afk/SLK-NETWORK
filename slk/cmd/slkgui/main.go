package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/slkproject/slk/core/chain"
	"github.com/slkproject/slk/core/state"
	"github.com/slkproject/slk/wallet"
)

var (
	bc         *chain.Blockchain
	mempool    *state.Mempool
	myWallet   *wallet.Wallet
	walletPath = os.Getenv("HOME") + "/.slk/wallet.json"

	balanceLabel *widget.Label
	peersLabel   *widget.Label
	statusLabel  *widget.Label
)

func main() {
	a := app.New()
	a.Settings().SetTheme(theme.DarkTheme())
	w := a.NewWindow("SLK — Proof of Race")
	w.Resize(fyne.NewSize(520, 720))
	w.SetFixedSize(false)

	myWallet, _ = wallet.LoadOrCreate(walletPath)
	bc = chain.NewBlockchain()
	mempool = state.NewMempool()
	myWallet.SyncBalance(bc.UTXOSet.GetTotalBalance(myWallet.Address))

	w.SetContent(makeMainScreen(w))
	w.ShowAndRun()
}

func makeMainScreen(w fyne.Window) fyne.CanvasObject {

	// ── TITLE ──
	title := canvas.NewText("SLK Network", theme.ForegroundColor())
	title.TextSize = 26
	title.TextStyle = fyne.TextStyle{Bold: true}
	title.Alignment = fyne.TextAlignCenter

	subtitle := canvas.NewText("Proof of Race Blockchain", theme.PlaceHolderColor())
	subtitle.TextSize = 13
	subtitle.Alignment = fyne.TextAlignCenter

	// ── BALANCE ──
	balanceTitle := widget.NewLabel("Total Balance")
	balanceTitle.Alignment = fyne.TextAlignCenter
	balanceTitle.TextStyle = fyne.TextStyle{Italic: true}

	balanceLabel = widget.NewLabel(fmt.Sprintf("%.8f SLK", myWallet.Balance))
	balanceLabel.Alignment = fyne.TextAlignCenter
	balanceLabel.TextStyle = fyne.TextStyle{Bold: true}

	// ── ADDRESS ──
	addrTitle := widget.NewLabel("Wallet Address")
	addrTitle.Alignment = fyne.TextAlignCenter
	addrTitle.TextStyle = fyne.TextStyle{Italic: true}

	addrLabel := widget.NewLabel(myWallet.Address)
	addrLabel.Alignment = fyne.TextAlignCenter
	addrLabel.Wrapping = fyne.TextWrapWord

	// ── NETWORK STATUS ──
	peersLabel = widget.NewLabel("🌍 Connecting to network...")
	peersLabel.Alignment = fyne.TextAlignCenter

	mempoolLabel := widget.NewLabel(fmt.Sprintf("📦 Mempool: %d pending", mempool.Size()))
	mempoolLabel.Alignment = fyne.TextAlignCenter

	chainLabel := widget.NewLabel(fmt.Sprintf("⛓  Chain Height: %d  |  Trophies: %d", bc.Height, len(bc.Trophies)))
	chainLabel.Alignment = fyne.TextAlignCenter

	statusLabel = widget.NewLabel("✅ Node ready")
	statusLabel.Alignment = fyne.TextAlignCenter

	// ── AUTO REFRESH ──
	lastBalance := myWallet.Balance
	go func() {
		for {
			time.Sleep(4 * time.Second)
			oldBal := lastBalance
			myWallet.SyncBalance(bc.UTXOSet.GetTotalBalance(myWallet.Address))
			newBal := myWallet.Balance
			lastBalance = newBal
			fyne.Do(func() {
				balanceLabel.SetText(fmt.Sprintf("%.8f SLK", myWallet.Balance))
				mempoolLabel.SetText(fmt.Sprintf("📦 Mempool: %d pending", mempool.Size()))
				chainLabel.SetText(fmt.Sprintf("⛓  Chain Height: %d  |  Trophies: %d", bc.Height, len(bc.Trophies)))
				// Show popup if balance increased
				if newBal > oldBal {
					received := newBal - oldBal
					statusLabel.SetText(fmt.Sprintf("📥 Received %.8f SLK!", received))
					dialog.ShowInformation("💰 SLK Received!", fmt.Sprintf("You received %.8f SLK\nNew Balance: %.8f SLK", received, newBal), w)
				}
			})
		}
	}()

	// ── BUTTONS ──
	raceBtn := widget.NewButton("🏁  Start Racing", func() {
		go func() {
			var cmd *exec.Cmd
			// Try common terminal emulators
			for _, term := range []string{"gnome-terminal", "xterm", "konsole", "xfce4-terminal"} {
				if _, err := exec.LookPath(term); err == nil {
					switch term {
					case "gnome-terminal":
						cmd = exec.Command(term, "--", "bash", "-c", "slkd; read -p 'Press enter to close'")
					default:
						cmd = exec.Command(term, "-e", "bash -c 'slkd; read -p Press enter to close'")
					}
					break
				}
			}
			if cmd == nil {
				fyne.Do(func() {
					dialog.ShowInformation("Start Racing",
						"Open a terminal and run:\n\n    slkd\n\nThen press [1] to start racing.",
						w)
				})
				return
			}
			cmd.Start()
			fyne.Do(func() {
				statusLabel.SetText("🏁 Racing started in terminal!")
			})
		}()
	})
	raceBtn.Importance = widget.HighImportance

	sendBtn := widget.NewButton("💸  Send SLK", func() {
		showSendDialog(w)
	})
	sendBtn.Importance = widget.MediumImportance

	chainBtn := widget.NewButton("🏆  Trophy Chain", func() {
		showChainDialog(w)
	})

	walletBtn := widget.NewButton("🔑  Wallet Info", func() {
		showWalletDialog(w)
	})

	copyBtn := widget.NewButton("📋  Copy My Address", func() {
		w.Clipboard().SetContent(myWallet.Address)
		fyne.Do(func() {
			statusLabel.SetText("✅ Address copied to clipboard!")
		})
		go func() {
			time.Sleep(3 * time.Second)
			fyne.Do(func() {
				statusLabel.SetText("✅ Node ready")
			})
		}()
	})

	// ── LAYOUT ──
	headerBox := container.NewVBox(
		container.NewCenter(title),
		container.NewCenter(subtitle),
		widget.NewSeparator(),
	)

	balanceBox := container.New(layout.NewVBoxLayout(),
		balanceTitle,
		container.NewCenter(balanceLabel),
	)

	addrBox := container.New(layout.NewVBoxLayout(),
		addrTitle,
		container.NewPadded(addrLabel),
	)

	networkBox := container.NewVBox(
		widget.NewSeparator(),
		peersLabel,
		chainLabel,
		mempoolLabel,
		widget.NewSeparator(),
	)

	btnBox := container.New(layout.NewVBoxLayout(),
		raceBtn,
		widget.NewSeparator(),
		sendBtn,
		container.NewGridWithColumns(2, chainBtn, walletBtn),
		copyBtn,
	)

	statusBox := container.NewVBox(
		widget.NewSeparator(),
		statusLabel,
	)

	return container.NewVBox(
		headerBox,
		container.NewPadded(balanceBox),
		container.NewPadded(addrBox),
		networkBox,
		container.NewPadded(btnBox),
		statusBox,
	)
}

func showSendDialog(w fyne.Window) {
	receiverEntry := widget.NewEntry()
	receiverEntry.SetPlaceHolder("SLK-xxxx-xxxx-xxxx-xxxx")

	amountEntry := widget.NewEntry()
	amountEntry.SetPlaceHolder("0.00000000")

	form := dialog.NewForm("💸 Send SLK", "Send", "Cancel",
		[]*widget.FormItem{
			widget.NewFormItem("To Address", receiverEntry),
			widget.NewFormItem("Amount (SLK)", amountEntry),
		},
		func(confirm bool) {
			if !confirm {
				return
			}
			receiver := receiverEntry.Text
			var amount float64
			fmt.Sscanf(amountEntry.Text, "%f", &amount)

			if receiver == "" || amount <= 0 {
				dialog.ShowError(fmt.Errorf("invalid address or amount"), w)
				return
			}
			if amount > myWallet.Balance {
				dialog.ShowError(fmt.Errorf("insufficient balance: you have %.8f SLK", myWallet.Balance), w)
				return
			}

			ts := time.Now().Unix()
			tx := wallet.Transaction{
				ID:        fmt.Sprintf("%x", ts),
				From:      myWallet.Address,
				To:        receiver,
				Amount:    amount,
				Timestamp: ts,
				Status:    "pending",
			}
			err := wallet.SignTransaction(&tx, myWallet)
			if err != nil {
				dialog.ShowError(err, w)
				return
			}

			senderUTXOs := bc.UTXOSet.GetUnspentForAddress(myWallet.Address)
			totalSpent := 0.0
			for _, utxo := range senderUTXOs {
				if totalSpent >= amount {
					break
				}
				bc.UTXOSet.SpendUTXO(utxo.TxID, utxo.OutputIndex, tx.ID)
				totalSpent += utxo.Amount
			}
			bc.UTXOSet.AddUTXO(&state.UTXO{
				TxID:        tx.ID,
				OutputIndex: 0,
				Amount:      amount,
				Address:     receiver,
				Spent:       false,
			})
			change := totalSpent - amount
			if change > 0.000000001 {
				bc.UTXOSet.AddUTXO(&state.UTXO{
					TxID:        tx.ID,
					OutputIndex: 1,
					Amount:      change,
					Address:     myWallet.Address,
					Spent:       false,
				})
			}
			bc.UTXOSet.Save()
			myWallet.SyncBalance(bc.UTXOSet.GetTotalBalance(myWallet.Address))
			myWallet.Save(walletPath)

			fyne.Do(func() {
				balanceLabel.SetText(fmt.Sprintf("%.8f SLK", myWallet.Balance))
				statusLabel.SetText(fmt.Sprintf("✅ Sent %.8f SLK to %s", amount, receiver[:16]))
			})

			dialog.ShowInformation("✅ Transaction Sent!",
				fmt.Sprintf("Amount:    %.8f SLK\nTo:        %s\nChange:    %.8f SLK returned to you\nTX ID:     %s",
					amount, receiver, change, tx.ID), w)
		}, w)

	form.Resize(fyne.NewSize(460, 200))
	form.Show()
}

func showChainDialog(w fyne.Window) {
	info := fmt.Sprintf("Chain Height:  %d\nTotal Trophies: %d\n\n", bc.Height, len(bc.Trophies))
	for i, t := range bc.Trophies {
		if i == 0 {
			continue
		}
		info += fmt.Sprintf("🏆 Block #%d\n  Winner:  %s\n  Reward:  %.8f SLK\n\n",
			t.Header.Height, t.Winner, t.Reward)
	}
	if len(bc.Trophies) <= 1 {
		info += "No trophies won yet.\nStart racing to win blocks!"
	}
	dialog.ShowInformation("🏆 Trophy Chain", info, w)
}

func showWalletDialog(w fyne.Window) {
	mnemonic := myWallet.Mnemonic
	if mnemonic == "" {
		mnemonic = "(not available — old wallet)"
	}
	info := fmt.Sprintf(
		"Address:\n  %s\n\nBalance:\n  %.8f SLK\n\n🔑 Seed Phrase (12 words):\n  %s\n\n⚠️  Never share your seed phrase!\n\nAlgorithm:\n  Ed25519\n\nStorage:\n  ~/.slk/wallet.json",
		myWallet.Address, myWallet.Balance, mnemonic)
	dialog.ShowInformation("🔑 Wallet Info", info, w)
}
