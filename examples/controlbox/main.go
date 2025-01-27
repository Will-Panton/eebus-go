package main

import (
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/enbility/eebus-go/api"
	"github.com/enbility/eebus-go/service"
	ucapi "github.com/enbility/eebus-go/usecases/api"
	"github.com/enbility/eebus-go/usecases/eg/lpc"
	"github.com/enbility/eebus-go/usecases/eg/lpp"
	shipapi "github.com/enbility/ship-go/api"
	"github.com/enbility/ship-go/cert"
	spineapi "github.com/enbility/spine-go/api"
	"github.com/enbility/spine-go/model"
	"github.com/gorilla/websocket"
)

var remoteSki string

type WebsocketClient struct {
	websocket *websocket.Conn
	mutex     sync.Mutex
}

func (websocketClient *WebsocketClient) sendMessage(msg interface{}) error {
	websocketClient.mutex.Lock()
	defer websocketClient.mutex.Unlock()

	err := websocketClient.websocket.WriteJSON(msg)
	if err != nil {
		log.Println(err)
	}
	return err
}

var frontend WebsocketClient

type failsafeLimits struct {
	Value    float64
	Duration time.Duration
}

var consumptionLimits ucapi.LoadLimit
var productionLimits ucapi.LoadLimit
var consumptionFailsafeLimits failsafeLimits
var productionFailsafeLimits failsafeLimits

type controlbox struct {
	myService *service.Service

	uclpc ucapi.EgLPCInterface
	uclpp ucapi.EgLPPInterface

	isConnected bool
}

func (h *controlbox) run() {
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
		"Demo", "Demo", "ControlBox", "123456789",
		[]shipapi.DeviceCategoryType{shipapi.DeviceCategoryTypeGridConnectionHub},
		model.DeviceTypeTypeElectricitySupplySystem,
		[]model.EntityTypeType{model.EntityTypeTypeGridGuard},
		port, certificate, time.Second*60)
	if err != nil {
		log.Fatal(err)
	}
	configuration.SetAlternateIdentifier("Demo-ControlBox-123456789")

	h.myService = service.NewService(configuration, h)
	h.myService.SetLogging(h)

	if err = h.myService.Setup(); err != nil {
		fmt.Println(err)
		return
	}

	consumptionLimits = ucapi.LoadLimit{
		IsActive: false,
		Value:    4200,
		Duration: 2 * time.Hour}

	productionLimits = ucapi.LoadLimit{
		IsActive: false,
		Value:    5000,
		Duration: 1 * time.Hour}

	consumptionFailsafeLimits = failsafeLimits{
		Value:    4200,
		Duration: 2 * time.Hour}

	productionFailsafeLimits = failsafeLimits{
		Value:    5000,
		Duration: 1 * time.Hour}

	localEntity := h.myService.LocalDevice().EntityForType(model.EntityTypeTypeGridGuard)
	h.uclpc = lpc.NewLPC(localEntity, h.OnLPCEvent)
	h.myService.AddUseCase(h.uclpc)

	h.uclpp = lpp.NewLPP(localEntity, h.OnLPPEvent)
	h.myService.AddUseCase(h.uclpp)

	if len(remoteSki) == 0 {
		os.Exit(0)
	}

	h.myService.RegisterRemoteSKI(remoteSki)

	h.myService.Start()
	// defer h.myService.Shutdown()
}

// EEBUSServiceHandler

func (h *controlbox) RemoteSKIConnected(service api.ServiceInterface, ski string) {
	h.isConnected = true
}

func (h *controlbox) RemoteSKIDisconnected(service api.ServiceInterface, ski string) {
	h.isConnected = false
}

func (h *controlbox) VisibleRemoteServicesUpdated(service api.ServiceInterface, entries []shipapi.RemoteService) {
}

func (h *controlbox) ServiceShipIDUpdate(ski string, shipdID string) {}

func (h *controlbox) ServicePairingDetailUpdate(ski string, detail *shipapi.ConnectionStateDetail) {
	if ski == remoteSki && detail.State() == shipapi.ConnectionStateRemoteDeniedTrust {
		fmt.Println("The remote service denied trust. Exiting.")
		h.myService.CancelPairingWithSKI(ski)
		h.myService.UnregisterRemoteSKI(ski)
		h.myService.Shutdown()
		os.Exit(0)
	}
}

func (h *controlbox) AllowWaitingForTrust(ski string) bool {
	return ski == remoteSki
}

// LPC Event Handler

func (h *controlbox) sendConsumptionLimit(entity spineapi.EntityRemoteInterface) {
	resultCB := func(msg model.ResultDataType) {
		if *msg.ErrorNumber == model.ErrorNumberTypeNoError {
			fmt.Println("Consumption limit accepted.")
		} else {
			fmt.Println("Consumption limit rejected. Code", *msg.ErrorNumber, "Description", *msg.Description)
		}
	}
	msgCounter, err := h.uclpc.WriteConsumptionLimit(entity, consumptionLimits, resultCB)
	if err != nil {
		fmt.Println("Failed to send consumption limit", err)
		return
	}
	fmt.Println("Sent consumption limit to", entity.Device().Ski(), "with msgCounter", msgCounter)
}

func (h *controlbox) sendConsumptionFailsafeLimit(entity spineapi.EntityRemoteInterface) {
	msgCounter, err := h.uclpc.WriteFailsafeConsumptionActivePowerLimit(entity, consumptionFailsafeLimits.Value)
	if err != nil {
		fmt.Println("Failed to send consumption failsafe limit", err)
		return
	}
	fmt.Println("Sent consumption failsafe limit to", entity.Device().Ski(), "with msgCounter", msgCounter)
}

func (h *controlbox) sendConsumptionFailsafeDuration(entity spineapi.EntityRemoteInterface) {
	msgCounter, err := h.uclpc.WriteFailsafeDurationMinimum(entity, consumptionFailsafeLimits.Duration)
	if err != nil {
		fmt.Println("Failed to send consumption failsafe duration", err)
		return
	}
	fmt.Println("Sent consumption failsafe duration to", entity.Device().Ski(), "with msgCounter", msgCounter)
}

func (h *controlbox) sendProductionFailsafeLimit(entity spineapi.EntityRemoteInterface) {
	msgCounter, err := h.uclpp.WriteFailsafeProductionActivePowerLimit(entity, productionFailsafeLimits.Value)
	if err != nil {
		fmt.Println("Failed to send production failsafe limit", err)
		return
	}
	fmt.Println("Sent production failsafe limit to", entity.Device().Ski(), "with msgCounter", msgCounter)
}

func (h *controlbox) sendProductionFailsafeDuration(entity spineapi.EntityRemoteInterface) {
	msgCounter, err := h.uclpp.WriteFailsafeDurationMinimum(entity, productionFailsafeLimits.Duration)
	if err != nil {
		fmt.Println("Failed to send production failsafe duration", err)
		return
	}
	fmt.Println("Sent production failsafe duration to", entity.Device().Ski(), "with msgCounter", msgCounter)
}

func (h *controlbox) readConsumptionNominalMax(entity spineapi.EntityRemoteInterface) {
	nominal, err := h.uclpc.ConsumptionNominalMax(entity)

	if err != nil {
		fmt.Println("Failed to get consumption nominal max", err)
		return
	}

	if frontend.websocket != nil {
		answer := Message{
			Type:  GetConsumptionNominalMax,
			Value: nominal}

		frontend.sendMessage(answer)
	}
}

func (h *controlbox) OnLPCEvent(ski string, device spineapi.DeviceRemoteInterface, entity spineapi.EntityRemoteInterface, event api.EventType) {
	if !h.isConnected {
		return
	}

	switch event {
	case lpc.UseCaseSupportUpdate:
		fmt.Println("Sending consumption limit in 5s...")

		time.AfterFunc(5*time.Second, func() {
			h.readConsumptionNominalMax(entity)
			h.sendConsumptionLimit(entity)
			h.sendConsumptionFailsafeLimit(entity)
			h.sendConsumptionFailsafeDuration(entity)
		})
	case lpc.DataUpdateLimit:
		if currentLimit, err := h.uclpc.ConsumptionLimit(entity); err == nil {
			consumptionLimits = currentLimit

			if currentLimit.IsActive {
				fmt.Println("New consumption limit received: active,", currentLimit.Value, "W,", currentLimit.Duration)
			} else {
				fmt.Println("New consumption limit received: inactive,", currentLimit.Value, "W,", currentLimit.Duration)
			}
			if frontend.websocket != nil {
				answer := Message{
					Type: GetConsumptionLimit,
					Limit: ucapi.LoadLimit{
						IsActive: currentLimit.IsActive,
						Duration: currentLimit.Duration / time.Second,
						Value:    currentLimit.Value}}

				frontend.sendMessage(answer)
			}
		}
	case lpc.DataUpdateFailsafeConsumptionActivePowerLimit:
		if limit, err := h.uclpc.FailsafeConsumptionActivePowerLimit(entity); err == nil {
			consumptionFailsafeLimits.Value = limit

			if frontend.websocket != nil {
				answer := Message{
					Type:  GetConsumptionFailsafeValue,
					Value: limit}

				frontend.sendMessage(answer)
			}
		}
	case lpc.DataUpdateFailsafeDurationMinimum:
		if duration, err := h.uclpc.FailsafeDurationMinimum(entity); err == nil {
			consumptionFailsafeLimits.Duration = duration

			if frontend.websocket != nil {
				answer := Message{
					Type:  GetConsumptionFailsafeDuration,
					Value: float64(duration / time.Second)}

				frontend.sendMessage(answer)
			}
		}
	case lpc.DataUpdateHeartbeat:
		if frontend.websocket != nil {
			answer := Message{
				Type: GetConsumptionHeartbeat}

			frontend.sendMessage(answer)
		}
	default:
		return
	}
}

// LPP Event Handler

func (h *controlbox) sendProductionLimit(entity spineapi.EntityRemoteInterface) {
	resultCB := func(msg model.ResultDataType) {
		if *msg.ErrorNumber == model.ErrorNumberTypeNoError {
			fmt.Println("Production limit accepted.")
		} else {
			fmt.Println("Production limit rejected. Code", *msg.ErrorNumber, "Description", *msg.Description)
		}
	}
	msgCounter, err := h.uclpp.WriteProductionLimit(entity, productionLimits, resultCB)
	if err != nil {
		fmt.Println("Failed to send production limit", err)
		return
	}
	fmt.Println("Sent production limit to", entity.Device().Ski(), "with msgCounter", msgCounter)
}

func (h *controlbox) sendProductiomFailsafeLimit(entity spineapi.EntityRemoteInterface) {
	msgCounter, err := h.uclpp.WriteFailsafeProductionActivePowerLimit(entity, productionFailsafeLimits.Value)
	if err != nil {
		fmt.Println("Failed to send consumption limit", err)
		return
	}
	fmt.Println("Sent production limit to", entity.Device().Ski(), "with msgCounter", msgCounter)
}

func (h *controlbox) sendProductiomFailsafeDuration(entity spineapi.EntityRemoteInterface) {
	msgCounter, err := h.uclpp.WriteFailsafeDurationMinimum(entity, productionFailsafeLimits.Duration)
	if err != nil {
		fmt.Println("Failed to send consumption limit", err)
		return
	}
	fmt.Println("Sent production limit to", entity.Device().Ski(), "with msgCounter", msgCounter)
}

func (h *controlbox) readProductionNominalMax(entity spineapi.EntityRemoteInterface) {
	nominal, err := h.uclpp.ProductionNominalMax(entity)

	if err != nil {
		fmt.Println("Failed to get production nominal max", err)
		return
	}

	if frontend.websocket != nil {
		answer := Message{
			Type:  GetProductionNominalMax,
			Value: nominal}

		frontend.sendMessage(answer)
	}
}

func (h *controlbox) OnLPPEvent(ski string, device spineapi.DeviceRemoteInterface, entity spineapi.EntityRemoteInterface, event api.EventType) {
	if !h.isConnected {
		return
	}

	switch event {
	case lpp.UseCaseSupportUpdate:
		fmt.Println("Sending production limit in 5s...")

		time.AfterFunc(5*time.Second, func() {
			h.readProductionNominalMax(entity)
			h.sendProductionLimit(entity)
			h.sendProductiomFailsafeLimit(entity)
			h.sendProductiomFailsafeDuration(entity)
		})
	case lpp.DataUpdateLimit:
		if currentLimit, err := h.uclpp.ProductionLimit(entity); err == nil {
			productionLimits = currentLimit

			if currentLimit.IsActive {
				fmt.Println("New production limit received: active,", currentLimit.Value, "W,", currentLimit.Duration)
			} else {
				fmt.Println("New production limit received: inactive,", currentLimit.Value, "W,", currentLimit.Duration)
			}

			if frontend.websocket != nil {
				answer := Message{
					Type: GetProductionLimit,
					Limit: ucapi.LoadLimit{
						IsActive: currentLimit.IsActive,
						Duration: currentLimit.Duration / time.Second,
						Value:    currentLimit.Value}}

				frontend.sendMessage(answer)
			}
		}
	case lpp.DataUpdateFailsafeProductionActivePowerLimit:
		if limit, err := h.uclpp.FailsafeProductionActivePowerLimit(entity); err == nil {
			productionFailsafeLimits.Value = limit

			if frontend.websocket != nil {
				answer := Message{
					Type:  GetProductionFailsafeValue,
					Value: limit}

				frontend.sendMessage(answer)
			}
		}
	case lpp.DataUpdateFailsafeDurationMinimum:
		if duration, err := h.uclpp.FailsafeDurationMinimum(entity); err == nil {
			productionFailsafeLimits.Duration = duration

			if frontend.websocket != nil {
				answer := Message{
					Type:  GetProductionFailsafeDuration,
					Value: float64(duration / time.Second)}

				frontend.sendMessage(answer)
			}
		}
	case lpp.DataUpdateHeartbeat:
		if frontend.websocket != nil {
			answer := Message{
				Type: GetProductionHeartbeat}

			frontend.sendMessage(answer)
		}
	default:
		return
	}
}

// main app
func usage() {
	fmt.Println("First Run:")
	fmt.Println("  go run /examples/controlbox/main.go <serverport>")
	fmt.Println()
	fmt.Println("General Usage:")
	fmt.Println("  go run /examples/controlbox/main.go <serverport> <remoteski> <crtfile> <keyfile>")
}

func main() {
	if len(os.Args) < 2 {
		usage()
		return
	}

	h := controlbox{}
	h.run()

	setupRoutes(&h)
	log.Fatal(http.ListenAndServe(":"+strconv.Itoa(httpdPort), nil))

	// Clean exit to make sure mdns shutdown is invoked
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	// User exit
}

// Logging interface

func (h *controlbox) Trace(args ...interface{}) {
	// h.print("TRACE", args...)
}

func (h *controlbox) Tracef(format string, args ...interface{}) {
	// h.printFormat("TRACE", format, args...)
}

func (h *controlbox) Debug(args ...interface{}) {
	// h.print("DEBUG", args...)
}

func (h *controlbox) Debugf(format string, args ...interface{}) {
	// h.printFormat("DEBUG", format, args...)
}

func (h *controlbox) Info(args ...interface{}) {
	h.print("INFO ", args...)
}

func (h *controlbox) Infof(format string, args ...interface{}) {
	h.printFormat("INFO ", format, args...)
}

func (h *controlbox) Error(args ...interface{}) {
	h.print("ERROR", args...)
}

func (h *controlbox) Errorf(format string, args ...interface{}) {
	h.printFormat("ERROR", format, args...)
}

func (h *controlbox) currentTimestamp() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

func (h *controlbox) print(msgType string, args ...interface{}) {
	value := fmt.Sprintln(args...)
	fmt.Printf("%s %s %s", h.currentTimestamp(), msgType, value)
}

func (h *controlbox) printFormat(msgType, format string, args ...interface{}) {
	value := fmt.Sprintf(format, args...)
	fmt.Println(h.currentTimestamp(), msgType, value)
}

// web frontend

const (
	httpdPort int = 7070
)

const (
	Text                           = 0
	QRCode                         = 1
	Acknowledge                    = 2
	SetConsumptionLimit            = 3
	GetConsumptionLimit            = 4
	SetProductionLimit             = 5
	GetProductionLimit             = 6
	SetConsumptionFailsafeValue    = 7
	GetConsumptionFailsafeValue    = 8
	SetConsumptionFailsafeDuration = 9
	GetConsumptionFailsafeDuration = 10
	SetProductionFailsafeValue     = 11
	GetProductionFailsafeValue     = 12
	SetProductionFailsafeDuration  = 13
	GetProductionFailsafeDuration  = 14
	GetConsumptionNominalMax       = 15
	GetProductionNominalMax        = 16
	GetConsumptionHeartbeat        = 17
	StopConsumptionHeartbeat       = 18
	StartConsumptionHeartbeat      = 19
	GetProductionHeartbeat         = 20
	StopProductionHeartbeat        = 21
	StartProductionHeartbeat       = 22
)

type Message struct {
	Type  int
	Text  string
	Limit ucapi.LoadLimit
	Value float64
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// allow connection from any host
		return true
	},
}

func setupRoutes(h *controlbox) {
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(h, w, r)
	})
}

func serveWs(h *controlbox, w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("upgrade error:", err)
		return
	}

	frontend = WebsocketClient{
		websocket: ws}

	log.Println("Client Connected")
	answer := Message{
		Type: QRCode,
		Text: h.myService.QRCodeText()}

	frontend.sendMessage(answer)

	answer = Message{
		Type: GetConsumptionLimit,
		Limit: ucapi.LoadLimit{
			IsActive: consumptionLimits.IsActive,
			Duration: consumptionLimits.Duration / time.Second,
			Value:    consumptionLimits.Value}}

	frontend.sendMessage(answer)

	answer = Message{
		Type: GetProductionLimit,
		Limit: ucapi.LoadLimit{
			IsActive: productionLimits.IsActive,
			Duration: productionLimits.Duration / time.Second,
			Value:    productionLimits.Value}}

	frontend.sendMessage(answer)

	answer = Message{
		Type:  GetConsumptionFailsafeValue,
		Value: consumptionFailsafeLimits.Value}

	frontend.sendMessage(answer)

	answer = Message{
		Type:  GetConsumptionFailsafeDuration,
		Value: float64(consumptionFailsafeLimits.Duration / time.Second)}

	frontend.sendMessage(answer)

	answer = Message{
		Type:  GetProductionFailsafeValue,
		Value: productionFailsafeLimits.Value}

	frontend.sendMessage(answer)

	answer = Message{
		Type:  GetProductionFailsafeDuration,
		Value: float64(productionFailsafeLimits.Duration / time.Second)}

	frontend.sendMessage(answer)

	reader(h, ws)
}

func reader(h *controlbox, ws *websocket.Conn) {
	for {
		// read in a message
		_, p, err := ws.ReadMessage()
		if err != nil {
			log.Println(err)
			return
		}
		// print out that message for clarity
		fmt.Println(string(p))

		data := Message{}
		json.Unmarshal([]byte(p), &data)

		if data.Type == SetConsumptionLimit {
			var limit = data.Limit

			consumptionLimits.IsActive = limit.IsActive
			consumptionLimits.Value = limit.Value
			consumptionLimits.Duration = limit.Duration * time.Second

			for _, remoteEntityScenario := range h.uclpc.RemoteEntitiesScenarios() {
				h.sendConsumptionLimit(remoteEntityScenario.Entity)
			}
		} else if data.Type == SetProductionLimit {
			var limit = data.Limit

			productionLimits.IsActive = limit.IsActive
			productionLimits.Value = limit.Value
			productionLimits.Duration = limit.Duration * time.Second

			for _, remoteEntityScenario := range h.uclpp.RemoteEntitiesScenarios() {
				h.sendProductionLimit(remoteEntityScenario.Entity)
			}
		} else if data.Type == SetConsumptionFailsafeValue {
			var limit = data.Value

			consumptionFailsafeLimits.Value = limit

			for _, remoteEntityScenario := range h.uclpc.RemoteEntitiesScenarios() {
				h.sendConsumptionFailsafeLimit(remoteEntityScenario.Entity)
			}
		} else if data.Type == SetConsumptionFailsafeDuration {
			var limit = data.Value

			consumptionFailsafeLimits.Duration = time.Duration(limit) * time.Second

			for _, remoteEntityScenario := range h.uclpc.RemoteEntitiesScenarios() {
				h.sendConsumptionFailsafeDuration(remoteEntityScenario.Entity)
			}
		} else if data.Type == SetProductionFailsafeValue {
			var limit = data.Value

			productionFailsafeLimits.Value = limit

			for _, remoteEntityScenario := range h.uclpp.RemoteEntitiesScenarios() {
				h.sendProductionFailsafeLimit(remoteEntityScenario.Entity)
			}
		} else if data.Type == SetProductionFailsafeDuration {
			var limit = data.Value

			productionFailsafeLimits.Duration = time.Duration(limit) * time.Second

			for _, remoteEntityScenario := range h.uclpp.RemoteEntitiesScenarios() {
				h.sendProductionFailsafeDuration(remoteEntityScenario.Entity)
			}
			// } else if data.Type == GetConsumptionNominalMax {
			// 	for _, remoteEntityScenario := range h.uclpp.RemoteEntitiesScenarios() {
			// 		nominal, err := h.uclpc.ConsumptionNominalMax(remoteEntityScenario.Entity)
			// 		if err == nil {
			// 			if frontend.websocket != nil {
			// 				answer := Message{
			// 					Type:  GetConsumptionNominalMax,
			// 					Value: nominal}

			// 				frontend.sendMessage(answer)
			// 			}
			// 		}
			// 	}
		} else if data.Type == StopConsumptionHeartbeat {
			h.uclpc.StopHeartbeat()
		} else if data.Type == StartConsumptionHeartbeat {
			h.uclpc.StartHeartbeat()
		}

		answer := Message{
			Type: Acknowledge}

		bytes, _ := json.Marshal(answer)
		if err := ws.WriteMessage(1, bytes); err != nil {
			log.Println(err)
			return
		}
	}
}
