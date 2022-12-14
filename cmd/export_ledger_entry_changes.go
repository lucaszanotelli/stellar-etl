package cmd

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/stellar/go/xdr"
	"github.com/stellar/stellar-etl/internal/input"
	"github.com/stellar/stellar-etl/internal/transform"
	"github.com/stellar/stellar-etl/internal/utils"
	"github.com/stellar/stellar-etl/internal/utils/verify"
)

const verifyBatchSize = 50000

var exportLedgerEntryChangesCmd = &cobra.Command{
	Use:   "export_ledger_entry_changes",
	Short: "This command exports the changes in accounts, offers, trustlines and liquidity pools.",
	Long: `This command instantiates a stellar-core instance and uses it to export about accounts, offers, trustlines and liquidity pools.
The information is exported in batches determined by the batch-size flag. Each exported file will include the changes to the 
relevant data type that occurred during that batch.

If the end-ledger is omitted, then the stellar-core node will continue running and exporting information as new ledgers are 
confirmed by the Stellar network. 

If no data type flags are set, then by default all of them are exported. If any are set, it is assumed that the others should not
be exported.`,
	Run: func(cmd *cobra.Command, args []string) {
		// endNum, strictExport, isTest, extra := utils.MustCommonFlags(cmd.Flags(), cmdLogger)
		endNum, strictExport, isTest, _ := utils.MustCommonFlags(cmd.Flags(), cmdLogger)
		cmdLogger.StrictExport = strictExport
		env := utils.GetEnvironmentDetails(isTest)
		archive, _ := utils.CreateHistoryArchiveClient(env.ArchiveURLs) // to verify ledger
		execPath, configPath, startNum, batchSize, outputFolder := utils.MustCoreFlags(cmd.Flags(), cmdLogger)
		exportAccounts, exportOffers, exportTrustlines, exportPools, exportBalances := utils.MustExportTypeFlags(cmd.Flags(), cmdLogger)
		// gcsBucket, gcpCredentials := utils.MustGcsFlags(cmd.Flags(), cmdLogger)
		ctx := context.Background()

		err := os.MkdirAll(outputFolder, os.ModePerm)
		if err != nil {
			cmdLogger.Fatalf("unable to mkdir %s: %v", outputFolder, err)
		}

		if batchSize <= 0 {
			cmdLogger.Fatalf("batch-size (%d) must be greater than 0", batchSize)
		}

		// If none of the export flags are set, then we assume that everything should be exported
		if !exportAccounts && !exportOffers && !exportTrustlines && !exportPools && !exportBalances {
			exportAccounts, exportOffers, exportTrustlines, exportPools, exportBalances = true, true, true, true, true
		}

		if configPath == "" && endNum == 0 {
			cmdLogger.Fatal("stellar-core needs a config file path when exporting ledgers continuously (endNum = 0)")
		}

		execPath, err = filepath.Abs(execPath)
		if err != nil {
			cmdLogger.Fatal("could not get absolute filepath for stellar-core executable: ", err)
		}

		configPath, err = filepath.Abs(configPath)
		if err != nil {
			cmdLogger.Fatal("could not get absolute filepath for the config file: ", err)
		}

		core, err := input.PrepareCaptiveCore(execPath, configPath, startNum, endNum, env)
		if err != nil {
			cmdLogger.Fatal("error creating a prepared captive core instance: ", err)
		}

		if endNum == 0 {
			endNum = math.MaxInt32
		}

		verifyOutputs := make(map[uint32]transform.TransformedOutputType, endNum)
		changeChan := make(chan input.ChangeBatch)
		closeChan := make(chan int)
		go input.StreamChanges(core, startNum, endNum, batchSize, changeChan, closeChan, env, cmdLogger)

		for {
			select {
			case <-closeChan:
				return
			case batch, ok := <-changeChan:
				if !ok {
					continue
				}
				var transformedOutputs transform.TransformedOutputType
				for entryType, changes := range batch.Changes {
					switch entryType {
					// case xdr.LedgerEntryTypeAccount:
					// 	for _, change := range changes {
					// 		entry, _, _, _ := utils.ExtractEntryFromChange(change)
					// 		if changed, err := change.AccountChangedExceptSigners(); err != nil {
					// 			cmdLogger.LogError(fmt.Errorf("unable to identify changed accounts: %v", err))
					// 			continue
					// 		} else if changed {
					// 			acc, err := transform.TransformAccount(change)
					// 			if err != nil {
					// 				cmdLogger.LogError(fmt.Errorf("error transforming account entry last updated at %d: %s", entry.LastModifiedLedgerSeq, err))
					// 				continue
					// 			}
					// 			transformedOutputs.Accounts = append(transformedOutputs.Accounts, acc)

					// 			if ok, actualLedger := utils.LedgerIsCheckpoint(entry.LastModifiedLedgerSeq); ok {
					// 				x := verifyOutputs[actualLedger]
					// 				x.Accounts = append(x.Accounts, acc)
					// 				verifyOutputs[actualLedger] = x
					// 			}
					// 		}
					// 		if change.AccountSignersChanged() {
					// 			signers, err := transform.TransformSigners(change)
					// 			if err != nil {
					// 				cmdLogger.LogError(fmt.Errorf("error transforming account signers from %d :%s", entry.LastModifiedLedgerSeq, err))
					// 				continue
					// 			}
					// 			for _, s := range signers {
					// 				transformedOutputs.Signers = append(transformedOutputs.Signers, s)

					// 				if ok, actualLedger := utils.LedgerIsCheckpoint(entry.LastModifiedLedgerSeq); ok {
					// 					x := verifyOutputs[actualLedger]
					// 					x.Signers = append(x.Signers, s)
					// 					verifyOutputs[actualLedger] = x
					// 				}
					// 			}
					// 		}
					// 	}
					// case xdr.LedgerEntryTypeClaimableBalance:
					// 	for _, change := range changes {
					// 		entry, _, _, _ := utils.ExtractEntryFromChange(change)
					// 		balance, err := transform.TransformClaimableBalance(change)
					// 		if err != nil {
					// 			cmdLogger.LogError(fmt.Errorf("error transforming balance entry last updated at %d: %s", entry.LastModifiedLedgerSeq, err))
					// 			continue
					// 		}
					// 		transformedOutputs.Claimable_balances = append(transformedOutputs.Claimable_balances, balance)

					// 		if ok, actualLedger := utils.LedgerIsCheckpoint(entry.LastModifiedLedgerSeq); ok {
					// 			x := verifyOutputs[actualLedger]
					// 			x.Claimable_balances = append(x.Claimable_balances, balance)
					// 			verifyOutputs[actualLedger] = x
					// 		}
					// 	}
					case xdr.LedgerEntryTypeOffer:
						for _, change := range changes {
							entry, _, _, _ := utils.ExtractEntryFromChange(change)
							offer, err := transform.TransformOffer(change)
							if err != nil {
								cmdLogger.LogError(fmt.Errorf("error transforming offer entry last updated at %d: %s", entry.LastModifiedLedgerSeq, err))
								continue
							}
							transformedOutputs.Offers = append(transformedOutputs.Offers, offer)

							if ok, actualLedger := utils.LedgerIsCheckpoint(entry.LastModifiedLedgerSeq); ok {
								x := verifyOutputs[actualLedger]
								x.Offers = append(x.Offers, offer)
								verifyOutputs[actualLedger] = x
							}
						}
						// case xdr.LedgerEntryTypeTrustline:
						// 	for _, change := range changes {
						// 		entry, _, _, _ := utils.ExtractEntryFromChange(change)
						// 		trust, err := transform.TransformTrustline(change)
						// 		if err != nil {
						// 			cmdLogger.LogError(fmt.Errorf("error transforming trustline entry last updated at %d: %s", entry.LastModifiedLedgerSeq, err))
						// 			continue
						// 		}
						// 		transformedOutputs.Trustlines = append(transformedOutputs.Trustlines, trust)

						// 		if ok, actualLedger := utils.LedgerIsCheckpoint(entry.LastModifiedLedgerSeq); ok {
						// 			x := verifyOutputs[actualLedger]
						// 			x.Trustlines = append(x.Trustlines, trust)
						// 			verifyOutputs[actualLedger] = x
						// 		}
						// 	}
						// case xdr.LedgerEntryTypeLiquidityPool:
						// 	for _, change := range changes {
						// 		entry, _, _, _ := utils.ExtractEntryFromChange(change)
						// 		pool, err := transform.TransformPool(change)
						// 		if err != nil {
						// 			cmdLogger.LogError(fmt.Errorf("error transforming liquidity pool entry last updated at %d: %s", entry.LastModifiedLedgerSeq, err))
						// 			continue
						// 		}
						// 		transformedOutputs.Liquidity_pools = append(transformedOutputs.Liquidity_pools, pool)

						// 		if ok, actualLedger := utils.LedgerIsCheckpoint(entry.LastModifiedLedgerSeq); ok {
						// 			x := verifyOutputs[actualLedger]
						// 			x.Liquidity_pools = append(x.Liquidity_pools, pool)
						// 			verifyOutputs[actualLedger] = x
						// 		}
						// 	}
					}
				}

				// err := exportTransformedData(batch.BatchStart, batch.BatchEnd, outputFolder, transformedOutputs, gcpCredentials, gcsBucket, extra)
				if err != nil {
					cmdLogger.LogError(err)
					continue
				}
			}

			for checkpointLedgers := range verifyOutputs {
				v, err := verify.VerifyState(ctx, verifyOutputs[checkpointLedgers], archive, checkpointLedgers, verifyBatchSize)
				if err != nil {
					panic(err)
				}

				print(v)
			}
		}
	},
}

// func exportTransformedData(
// 	start, end uint32,
// 	folderPath string,
// 	transformedOutput transform.TransformedOutputType,
// 	gcpCredentials, gcsBucket string,
// 	extra map[string]string) error {

// 	values := reflect.ValueOf(transformedOutput)
// 	typesOf := values.Type()

// 	for i := 0; i < values.NumField(); i++ {
// 		// Filenames are typically exclusive of end point. This processor
// 		// is different and we have to increment by 1 since the end batch number
// 		// is included in this filename.
// 		path := filepath.Join(folderPath, exportFilename(start, end+1, typesOf.Field(i).Name))
// 		outFile := mustOutFile(path)

// 		output := (values.Field(i).Interface()).([]interface{})
// 		for _, o := range output {
// 			_, err := exportEntry(o, outFile, extra)
// 			if err != nil {
// 				return err
// 			}
// 		}
// 		maybeUpload(gcpCredentials, gcsBucket, path)
// 	}

// 	return nil
// }

func init() {
	rootCmd.AddCommand(exportLedgerEntryChangesCmd)
	utils.AddCommonFlags(exportLedgerEntryChangesCmd.Flags())
	utils.AddCoreFlags(exportLedgerEntryChangesCmd.Flags(), "changes_output/")
	utils.AddExportTypeFlags(exportLedgerEntryChangesCmd.Flags())
	utils.AddGcsFlags(exportLedgerEntryChangesCmd.Flags())

	exportLedgerEntryChangesCmd.MarkFlagRequired("start-ledger")
	exportLedgerEntryChangesCmd.MarkFlagRequired("core-executable")
	/*
		Current flags:
			start-ledger: the ledger sequence number for the beginning of the export period
			end-ledger: the ledger sequence number for the end of the export range

			output-folder: folder that will contain the output files
			limit: maximum number of changes to export in a given batch; if negative then everything gets exported
			batch-size: size of the export batches

			core-executable: path to stellar-core executable
			core-config: path to stellar-core config file

			If none of the export_X flags are set, assume everything should be exported
				export_accounts: boolean flag; if set then accounts should be exported
				export_trustlines: boolean flag; if set then trustlines should be exported
				export_offers: boolean flag; if set then offers should be exported

		TODO: implement extra flags if possible
			serialize-method: the method for serialization of the output data (JSON, XDR, etc)
			start and end time as a replacement for start and end sequence numbers
	*/
}
