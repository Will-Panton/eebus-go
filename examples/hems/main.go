package main

import (
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/enbility/eebus-go/api"
	"github.com/enbility/eebus-go/service"
	ucapi "github.com/enbility/eebus-go/usecases/api"
	"github.com/enbility/eebus-go/usecases/cem/vabd"
	"github.com/enbility/eebus-go/usecases/cem/vapd"
	cslpc "github.com/enbility/eebus-go/usecases/cs/lpc"
	cslpp "github.com/enbility/eebus-go/usecases/cs/lpp"
	gcpmgcp "github.com/enbility/eebus-go/usecases/gcp/mgcp"
	mumpc "github.com/enbility/eebus-go/usecases/mu/mpc"
	shipapi "github.com/enbility/ship-go/api"
	"github.com/enbility/ship-go/cert"
	spineapi "github.com/enbility/spine-go/api"
	"github.com/enbility/spine-go/model"
)

var remoteSki string

type hems struct {
	myService *service.Service

	uccslpc ucapi.CsLPCInterface
	uccslpp ucapi.CsLPPInterface
	// uceglpc   ucapi.EgLPCInterface
	// uceglpp   ucapi.EgLPPInterface
	ucgcpmgcp ucapi.GcpMGCPInterface
	ucmumpc   ucapi.MuMPCInterface

	uccemvabd ucapi.CemVABDInterface
	uccemvapd ucapi.CemVAPDInterface

	gridPowerLimitFactor float64
	gridPower            float64
	gridPowerPerPhase    []float64
	gridConsumedEnergy   float64
	gridFeedInEnergy     float64
	gridCurrentPerPhase  []float64
	gridVoltagePerPhase  []float64
	gridFrequency        float64
}

func (h *hems) run() {
	var err error
	var certificate tls.Certificate

	if len(os.Args) == 5 {
		remoteSki = os.Args[2]

		certificate, err = tls.LoadX509KeyPair(os.Args[3], os.Args[4])
		if err != nil {
			usage()
			log.Fatal(err)
		}
	} else {
		certificate, err = cert.CreateCertificate("Demo", "Demo", "DE", "Demo-Unit-01")
		if err != nil {
			log.Fatal(err)
		}

		pemdata := pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: certificate.Certificate[0],
		})
		fmt.Println(string(pemdata))

		b, err := x509.MarshalECPrivateKey(certificate.PrivateKey.(*ecdsa.PrivateKey))
		if err != nil {
			log.Fatal(err)
		}
		pemdata = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: b})
		fmt.Println(string(pemdata))
	}

	port, err := strconv.Atoi(os.Args[1])
	if err != nil {
		usage()
		log.Fatal(err)
	}

	configuration, err := api.NewConfiguration(
		"Demo", "Demo", "HEMS", "123456789",
		[]shipapi.DeviceCategoryType{shipapi.DeviceCategoryTypeEnergyManagementSystem},
		model.DeviceTypeTypeEnergyManagementSystem,
		[]model.EntityTypeType{model.EntityTypeTypeCEM, model.EntityTypeTypeSubMeterElectricity},
		port, certificate, time.Second*4)
	if err != nil {
		log.Fatal(err)
	}
	configuration.SetAlternateIdentifier("Demo-HEMS-123456789")

	h.myService = service.NewService(configuration, h)
	h.myService.SetLogging(h)

	if err = h.myService.Setup(); err != nil {
		fmt.Println(err)
		return
	}

	localEntityCEM := h.myService.LocalDevice().EntityForType(model.EntityTypeTypeCEM)
	h.uccslpc = cslpc.NewLPC(localEntityCEM, h.OnLPCEvent)
	h.myService.AddUseCase(h.uccslpc)
	h.uccslpp = cslpp.NewLPP(localEntityCEM, h.OnLPPEvent)
	h.myService.AddUseCase(h.uccslpp)
	// h.uceglpc = eglpc.NewLPC(localEntityCEM, nil)
	// h.myService.AddUseCase(h.uceglpc)
	// h.uceglpp = eglpp.NewLPP(localEntityCEM, nil)
	// h.myService.AddUseCase(h.uceglpp)
	h.ucgcpmgcp = gcpmgcp.NewMGCP(localEntityCEM, h.OnMGCPEvent)
	h.myService.AddUseCase(h.ucgcpmgcp)
	h.uccemvabd = vabd.NewVABD(localEntityCEM, h.OnVABDEvent)
	h.myService.AddUseCase(h.uccemvabd)
	h.uccemvapd = vapd.NewVAPD(localEntityCEM, h.OnVAPDEvent)
	h.myService.AddUseCase(h.uccemvapd)

	localEntitySME := h.myService.LocalDevice().EntityForType(model.EntityTypeTypeSubMeterElectricity)
	h.ucmumpc = mumpc.NewMPC(localEntitySME, h.OnMPCEvent)
	h.myService.AddUseCase(h.ucmumpc)

	// Initialize local server data
	_ = h.uccslpc.SetConsumptionLimit(ucapi.LoadLimit{
		Value:        4200,
		Duration:     2 * time.Hour,
		IsChangeable: true,
		IsActive:     false,
	})
	_ = h.uccslpc.SetFailsafeConsumptionActivePowerLimit(4200, true)
	_ = h.uccslpc.SetFailsafeDurationMinimum(2*time.Hour, true)

	_ = h.uccslpp.SetProductionLimit(ucapi.LoadLimit{
		Value:        3000,
		Duration:     2 * time.Hour,
		IsChangeable: true,
		IsActive:     false,
	})
	_ = h.uccslpp.SetFailsafeProductionActivePowerLimit(3000, true)
	_ = h.uccslpp.SetFailsafeDurationMinimum(2*time.Hour, true)

	//_ = h.ucmamgcp.
	if len(remoteSki) == 0 {
		os.Exit(0)
	}

	h.gridPowerLimitFactor = 70
	h.gridPower = 3000
	h.gridPowerPerPhase = []float64{900, 1000, 1100}
	h.gridConsumedEnergy = 12345
	h.gridFeedInEnergy = -1000
	h.gridCurrentPerPhase = []float64{10, 20, 30}
	h.gridVoltagePerPhase = []float64{229, 230, 231}
	h.gridFrequency = 50

	ticker := time.NewTicker(3000 * time.Millisecond)
	go func() {
		for range ticker.C {
			_ = h.ucgcpmgcp.SetPower(math.Abs(h.gridPower))
			_ = h.ucmumpc.SetPower(math.Abs(h.gridPower))
			switch h.gridPower {
			case 3000:
				h.gridPower = 3100
			case 3100:
				h.gridPower = -3000
			case -3000:
				h.gridPower = 2900
			case 2900:
				h.gridPower = 3000
			}

			_ = h.ucmumpc.SetPowerPerPhase(h.gridPowerPerPhase)
			tmp := h.gridPowerPerPhase[0]
			h.gridPowerPerPhase[0] = h.gridPowerPerPhase[1]
			h.gridPowerPerPhase[1] = h.gridPowerPerPhase[2]
			h.gridPowerPerPhase[2] = tmp

			_ = h.ucgcpmgcp.SetEnergyConsumed(h.gridConsumedEnergy)
			_ = h.ucmumpc.SetEnergyConsumed(h.gridConsumedEnergy)
			h.gridConsumedEnergy++

			_ = h.ucgcpmgcp.SetEnergyFeedIn(h.gridFeedInEnergy)
			_ = h.ucmumpc.SetEnergyProduced(h.gridFeedInEnergy)
			h.gridFeedInEnergy--

			_ = h.ucgcpmgcp.SetCurrentPerPhase(h.gridCurrentPerPhase)
			_ = h.ucmumpc.SetCurrentPerPhase(h.gridCurrentPerPhase)
			tmp = h.gridCurrentPerPhase[0]
			h.gridCurrentPerPhase[0] = h.gridCurrentPerPhase[1]
			h.gridCurrentPerPhase[1] = h.gridCurrentPerPhase[2]
			h.gridCurrentPerPhase[2] = tmp

			_ = h.ucgcpmgcp.SetVoltagePerPhase(h.gridVoltagePerPhase)
			_ = h.ucmumpc.SetVoltagePerPhase(h.gridVoltagePerPhase)
			tmp = h.gridVoltagePerPhase[0]
			h.gridVoltagePerPhase[0] = h.gridVoltagePerPhase[1]
			h.gridVoltagePerPhase[1] = h.gridVoltagePerPhase[2]
			h.gridVoltagePerPhase[2] = tmp

			_ = h.ucgcpmgcp.SetFrequency(math.Abs(h.gridFrequency))
			_ = h.ucmumpc.SetFrequency(math.Abs(h.gridFrequency))
			switch h.gridFrequency {
			case 50:
				h.gridFrequency = 51
			case 51:
				h.gridFrequency = -50
			case -50:
				h.gridFrequency = 49
			case 49:
				h.gridFrequency = 50
			}
		}
	}()

	//h.myService.RegisterRemoteSKI(remoteSki)
	h.myService.UserIsAbleToApproveOrCancelPairingRequests(true)

	h.myService.Start()
	// defer h.myService.Shutdown()
}

// Controllable System LPC Event Handler

func (h *hems) OnLPCEvent(ski string, device spineapi.DeviceRemoteInterface, entity spineapi.EntityRemoteInterface, event api.EventType) {
	switch event {
	case cslpc.WriteApprovalRequired:
		// get pending writes
		pendingWrites := h.uccslpc.PendingConsumptionLimits()

		// approve any write
		for msgCounter, write := range pendingWrites {
			fmt.Println("Approving LPC write with msgCounter", msgCounter, "and limit", write.Value, "W")
			//h.uccslpc.ApproveOrDenyConsumptionLimit(msgCounter, true, "")
			h.uccslpc.ApproveOrDenyConsumptionLimit(msgCounter, false, "I’m not in the mood right now.")
		}
	case cslpc.DataUpdateLimit:
		if currentLimit, err := h.uccslpc.ConsumptionLimit(); err == nil {
			fmt.Println("New LPC Limit set to", currentLimit.Value, "W")
		}
	case cslpc.DataUpdateFailsafeConsumptionActivePowerLimit:
		if currentLimit, changeable, err := h.uccslpc.FailsafeConsumptionActivePowerLimit(); err == nil {
			fmt.Println("New LPC Failsafe Limit set to", currentLimit, "W")
			if changeable {
				fmt.Println("New LPC Failsafe Limit set changeable")
			} else {
				fmt.Println("New LPC Failsafe Limit set not changeable")
			}
		}
	case cslpc.DataUpdateFailsafeDurationMinimum:
		if currentDuration, changeable, err := h.uccslpc.FailsafeDurationMinimum(); err == nil {
			fmt.Println("New LPC Failsafe Duration set to", currentDuration)
			if changeable {
				fmt.Println("New LPC Failsafe Duration set changeable")
			} else {
				fmt.Println("New LPC Failsafe Duration set not changeable")
			}
		}
	case cslpc.DataUpdateHeartbeat:
	}
}

// Controllable System LPP Event Handler

func (h *hems) OnLPPEvent(ski string, device spineapi.DeviceRemoteInterface, entity spineapi.EntityRemoteInterface, event api.EventType) {
	switch event {
	case cslpp.WriteApprovalRequired:
		// get pending writes
		pendingWrites := h.uccslpp.PendingProductionLimits()

		// approve any write
		for msgCounter, write := range pendingWrites {
			fmt.Println("Approving LPP write with msgCounter", msgCounter, "and limit", write.Value, "W")
			h.uccslpp.ApproveOrDenyProductionLimit(msgCounter, true, "")
		}
	case cslpp.DataUpdateLimit:
		if currentLimit, err := h.uccslpp.ProductionLimit(); err == nil {
			fmt.Println("New LPP Limit set to", currentLimit.Value, "W")
		}
	case cslpp.DataUpdateFailsafeProductionActivePowerLimit:
		if currentLimit, changeable, err := h.uccslpp.FailsafeProductionActivePowerLimit(); err == nil {
			fmt.Println("New LPP Failsafe Limit set to", currentLimit, "W")
			if changeable {
				fmt.Println("New LPP Failsafe Limit set changeable")
			} else {
				fmt.Println("New LPP Failsafe Limit set not changeable")
			}
		}
	case cslpp.DataUpdateFailsafeDurationMinimum:
		if currentDuration, changeable, err := h.uccslpp.FailsafeDurationMinimum(); err == nil {
			fmt.Println("New LPP Failsafe Duration set to", currentDuration)
			if changeable {
				fmt.Println("New LPP Failsafe Duration set changeable")
			} else {
				fmt.Println("New LPP Failsafe Duration set not changeable")
			}
		}
	case cslpp.DataUpdateHeartbeat:
	}
}

// Cem VABD Event Handler

func (h *hems) OnVABDEvent(ski string, device spineapi.DeviceRemoteInterface, entity spineapi.EntityRemoteInterface, event api.EventType) {
	switch event {
	case vabd.DataUpdateEnergyCharged:
		if energy, err := h.uccemvabd.EnergyCharged(entity); err == nil {
			fmt.Println("New VABD Energy Charged set to", energy, "Wh")
		}
	case vabd.DataUpdateEnergyDischarged:
		if energy, err := h.uccemvabd.EnergyDischarged(entity); err == nil {
			fmt.Println("New VABD Energy Discharged set to", energy, "Wh")
		}
	case vabd.DataUpdatePower:
		if power, err := h.uccemvabd.Power(entity); err == nil {
			fmt.Println("New VABD Power set to", power, "W")
		}
	case vabd.DataUpdateStateOfCharge:
		if soc, err := h.uccemvabd.StateOfCharge(entity); err == nil {
			fmt.Println("New VABD State of Charge set to", soc, "%")
		}
	}
}

// Cem VAPD Event Handler

func (h *hems) OnVAPDEvent(ski string, device spineapi.DeviceRemoteInterface, entity spineapi.EntityRemoteInterface, event api.EventType) {
	switch event {
	case vapd.DataUpdatePVYieldTotal:
		if yield, err := h.uccemvapd.PVYieldTotal(entity); err == nil {
			fmt.Println("New VAPD PV Yield Total set to", yield, "Wh")
		}
	case vapd.DataUpdatePowerNominalPeak:
		if peak, err := h.uccemvapd.PowerNominalPeak(entity); err == nil {
			fmt.Println("New VAPD Power Nominal Peak set to", peak, "W")
		}
	case vapd.DataUpdatePower:
		if power, err := h.uccemvapd.Power(entity); err == nil {
			fmt.Println("New VAPD Power set to", power, "W")
		}
	}
}

// Monitoring Appliance MGCP Event Handler

func (h *hems) OnMGCPEvent(ski string, device spineapi.DeviceRemoteInterface, entity spineapi.EntityRemoteInterface, event api.EventType) {
}

// Monitored Unit MPC Event Handler

func (h *hems) OnMPCEvent(ski string, device spineapi.DeviceRemoteInterface, entity spineapi.EntityRemoteInterface, event api.EventType) {
}

// EEBUSServiceHandler

func (h *hems) RemoteSKIConnected(service api.ServiceInterface, ski string) {
	fmt.Println("RemoteSKIConnected: ", ski)

	time.AfterFunc(1*time.Second, func() {
		fmt.Println("---- SetNominalMax ----")
		_ = h.uccslpc.SetConsumptionNominalMax(34500)
		_ = h.uccslpp.SetProductionNominalMax(10000)
		_ = h.ucgcpmgcp.SetPowerLimitationFactor(h.gridPowerLimitFactor)
	})

}

func (h *hems) RemoteSKIDisconnected(service api.ServiceInterface, ski string) {
	fmt.Println("RemoteSKIDisconnected: " + ski)
}

func (h *hems) VisibleRemoteServicesUpdated(service api.ServiceInterface, entries []shipapi.RemoteService) {
	fmt.Print("VisibleRemoteServicesUpdated, count: ")
	fmt.Println(len(entries))

	for _, element := range entries {
		fmt.Println("Remote SKI: " + element.Ski)
		service := h.myService.RemoteServiceForSKI(element.Ski)
		service.SetTrusted(true)
	}
}

func (h *hems) ServiceShipIDUpdate(ski string, shipdID string) {}

func (h *hems) ServicePairingDetailUpdate(ski string, detail *shipapi.ConnectionStateDetail) {
	states := []string{"ConnectionStateNone", "ConnectionStateQueued", "ConnectionStateInitiated",
		"ConnectionStateReceivedPairingRequest", "ConnectionStateInProgress", "ConnectionStateTrusted",
		"ConnectionStatePin", "ConnectionStateCompleted", "ConnectionStateRemoteDeniedTrust", "ConnectionStateError",
	}

	if detail.Error() == nil {
		fmt.Println("ServicePairingDetailUpdate: " + ski + ", " + states[detail.State()])
	} else {
		fmt.Println("ServicePairingDetailUpdate: " + ski + ", " + states[detail.State()] + ", " + detail.Error().Error())
	}

	if detail.State() == shipapi.ConnectionStateRemoteDeniedTrust {
		fmt.Println("The remote service denied trust. Exiting.")
		h.myService.CancelPairingWithSKI(ski)
		h.myService.UnregisterRemoteSKI(ski)
		h.myService.Shutdown()
		os.Exit(0)
	}
}

func (h *hems) AllowWaitingForTrust(ski string) bool {
	//return ski == remoteSki
	return true
}

// UCEvseCommisioningConfigurationCemDelegate

// handle device state updates from the remote EVSE device
func (h *hems) HandleEVSEDeviceState(ski string, failure bool, errorCode string) {
	fmt.Println("EVSE Error State:", failure, errorCode)
}

// main app
func usage() {
	fmt.Println("First Run:")
	fmt.Println("  go run /examples/hems/main.go <serverport>")
	fmt.Println()
	fmt.Println("General Usage:")
	fmt.Println("  go run /examples/hems/main.go <serverport> <remoteski> <crtfile> <keyfile>")
}

func main() {
	if len(os.Args) < 2 {
		usage()
		return
	}

	h := hems{}
	h.run()

	// Clean exit to make sure mdns shutdown is invoked
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	// User exit
}

// Logging interface

func (h *hems) Trace(args ...interface{}) {
	//h.print("TRACE", args...)
}

func (h *hems) Tracef(format string, args ...interface{}) {
	//h.printFormat("TRACE", format, args...)
}

func (h *hems) Debug(args ...interface{}) {
	//h.print("DEBUG", args...)
}

func (h *hems) Debugf(format string, args ...interface{}) {
	//h.printFormat("DEBUG", format, args...)
}

func (h *hems) Info(args ...interface{}) {
	h.print("INFO ", args...)
}

func (h *hems) Infof(format string, args ...interface{}) {
	h.printFormat("INFO ", format, args...)
}

func (h *hems) Error(args ...interface{}) {
	h.print("ERROR", args...)
}

func (h *hems) Errorf(format string, args ...interface{}) {
	h.printFormat("ERROR", format, args...)
}

func (h *hems) currentTimestamp() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

func (h *hems) print(msgType string, args ...interface{}) {
	value := fmt.Sprintln(args...)
	fmt.Printf("%s %s %s", h.currentTimestamp(), msgType, value)
}

func (h *hems) printFormat(msgType, format string, args ...interface{}) {
	value := fmt.Sprintf(format, args...)
	fmt.Println(h.currentTimestamp(), msgType, value)
}
