package main

import (
	"encoding/json"
	"log"
	"net/http"
	"slices"
	"time"

	ucapi "github.com/enbility/eebus-go/usecases/api"
	shipapi "github.com/enbility/ship-go/api"
	spineapi "github.com/enbility/spine-go/api"
	"github.com/gorilla/websocket"
)

// web frontend

const (
	httpdPort int = 7080
)

const (
	Text                           = 0
	QRCode                         = 1
	Acknowledge                    = 2
	ServiceListChanged             = 3
	GetServiceList                 = 4
	SelectService                  = 5
	GetEntityInfos                 = 6
	GetAllData                     = 7
	SetConsumptionLimit            = 8
	GetConsumptionLimit            = 9
	SetProductionLimit             = 10
	GetProductionLimit             = 11
	SetConsumptionFailsafeValue    = 12
	GetConsumptionFailsafeValue    = 13
	SetConsumptionFailsafeDuration = 14
	GetConsumptionFailsafeDuration = 15
	SetProductionFailsafeValue     = 16
	GetProductionFailsafeValue     = 17
	SetProductionFailsafeDuration  = 18
	GetProductionFailsafeDuration  = 19
	GetConsumptionNominalMax       = 20
	GetProductionNominalMax        = 21
	GetConsumptionHeartbeat        = 22
	StopConsumptionHeartbeat       = 23
	StartConsumptionHeartbeat      = 24
	GetProductionHeartbeat         = 25
	StopProductionHeartbeat        = 26
	StartProductionHeartbeat       = 27
	GetPowerLimitationFactor       = 28
	GetPower                       = 29
	GetEnergyFeedIn                = 30
	GetEnergyConsumed              = 31
	GetCurrentPerPhase             = 32
	GetVoltagePerPhase             = 33
	GetFrequency                   = 34
)

type RemoteInfo struct {
	Service  shipapi.RemoteService
	Device   spineapi.DeviceRemoteInterface
	UseCases []string
}

type EntityInfo struct {
	Name     string
	SKI      string
	Type     string
	Features []string
	UseCases []string
}

type Message struct {
	Type        int
	Text        string
	Limit       ucapi.LoadLimit
	Value       float64
	Values      []float64
	ServiceList []shipapi.RemoteService
	EntityInfos []EntityInfo
	UseCase     string
}

func readData(h *controlbox, entity spineapi.EntityRemoteInterface, ucs []string) {
	if (ucs == nil || slices.Contains(ucs, "LPC")) && slices.Contains(h.remoteInfos[entity.Device().Ski()].UseCases, "LPC") {
		if currentLimit, err := h.uclpc.ConsumptionLimit(entity); err == nil {
			h.consumptionLimits = currentLimit

			frontend.sendLimit(GetConsumptionLimit, "LPC", ucapi.LoadLimit{
				IsActive: currentLimit.IsActive,
				Duration: currentLimit.Duration / time.Second,
				Value:    currentLimit.Value})
		}

		if limit, err := h.uclpc.FailsafeConsumptionActivePowerLimit(entity); err == nil {
			h.consumptionFailsafeLimits.Value = limit

			frontend.sendValue(GetConsumptionFailsafeValue, "LPC", limit)
		}

		if duration, err := h.uclpc.FailsafeDurationMinimum(entity); err == nil {
			h.consumptionFailsafeLimits.Duration = duration

			frontend.sendValue(GetConsumptionFailsafeDuration, "LPC", float64(duration/time.Second))
		}

		if nominal, err := h.uclpc.ConsumptionNominalMax(entity); err == nil {
			h.consumptionNominalMax = nominal

			frontend.sendValue(GetConsumptionNominalMax, "LPC", nominal)
		}
	}

	if (ucs == nil || slices.Contains(ucs, "LPP")) && slices.Contains(h.remoteInfos[entity.Device().Ski()].UseCases, "LPP") {
		if currentLimit, err := h.uclpp.ProductionLimit(entity); err == nil {
			h.productionLimits = currentLimit

			frontend.sendLimit(GetProductionLimit, "LPP", ucapi.LoadLimit{
				IsActive: currentLimit.IsActive,
				Duration: currentLimit.Duration / time.Second,
				Value:    currentLimit.Value})
		}

		if limit, err := h.uclpp.FailsafeProductionActivePowerLimit(entity); err == nil {
			h.productionFailsafeLimits.Value = limit

			frontend.sendValue(GetProductionFailsafeValue, "LPP", limit)
		}

		if duration, err := h.uclpp.FailsafeDurationMinimum(entity); err == nil {
			h.productionFailsafeLimits.Duration = duration

			frontend.sendValue(GetProductionFailsafeDuration, "LPP", float64(duration/time.Second))
		}

		if nominal, err := h.uclpp.ProductionNominalMax(entity); err == nil {
			h.productionNominalMax = nominal

			frontend.sendValue(GetProductionNominalMax, "LPP", nominal)
		}
	}
}

func sendData(h *controlbox) {
	frontend.sendText(QRCode, h.myService.QRCodeText())

	frontend.sendLimit(GetConsumptionLimit, "LPC", ucapi.LoadLimit{
		IsActive: h.consumptionLimits.IsActive,
		Duration: h.consumptionLimits.Duration / time.Second,
		Value:    h.consumptionLimits.Value})

	frontend.sendValue(GetConsumptionFailsafeValue, "LPC", h.consumptionFailsafeLimits.Value)

	frontend.sendValue(GetConsumptionFailsafeDuration, "LPC", float64(h.consumptionFailsafeLimits.Duration/time.Second))

	frontend.sendLimit(GetProductionLimit, "LPP", ucapi.LoadLimit{
		IsActive: h.productionLimits.IsActive,
		Duration: h.productionLimits.Duration / time.Second,
		Value:    h.productionLimits.Value})

	frontend.sendValue(GetProductionFailsafeValue, "LPP", h.productionFailsafeLimits.Value)

	frontend.sendValue(GetProductionFailsafeDuration, "LPP", float64(h.productionFailsafeLimits.Duration/time.Second))
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

func enableCors(w *http.ResponseWriter) {
	(*w).Header().Set("Access-Control-Allow-Origin", "*")
}

func serveWs(h *controlbox, w http.ResponseWriter, r *http.Request) {
	enableCors(&w)

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("upgrade error:", err)
		return
	}

	frontend = WebsocketClient{
		websocket: ws}

	log.Println("Client Connected")

	frontend.sendServiceList(GetServiceList, h.currentRemoteServices)

	sendData(h)

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
		//fmt.Println(string(p))

		data := Message{}
		json.Unmarshal([]byte(p), &data)

		switch data.Type {
		case GetServiceList:
			frontend.sendServiceList(GetServiceList, h.currentRemoteServices)
		case SelectService:
			remoteSki = data.Text

			info, exists := h.remoteInfos[remoteSki]
			if !exists {
				h.myService.RegisterRemoteSKI(remoteSki)
			} else {
				for _, entity := range info.Device.Entities() {
					readData(h, entity, nil)
				}
			}
		case GetEntityInfos:
			if nil != h.remoteInfos {
				frontend.sendEntityInfo(GetEntityInfos, h.remoteInfos)
			}
		case GetAllData:
			sendData(h)
		case SetConsumptionLimit:
			var limit = data.Limit

			h.consumptionLimits.IsActive = limit.IsActive
			h.consumptionLimits.Value = limit.Value
			h.consumptionLimits.Duration = limit.Duration * time.Second

			for _, remoteEntityScenario := range h.uclpc.RemoteEntitiesScenarios() {
				h.sendConsumptionLimit(remoteEntityScenario.Entity)
			}
		case SetProductionLimit:
			var limit = data.Limit

			h.productionLimits.IsActive = limit.IsActive
			h.productionLimits.Value = limit.Value
			h.productionLimits.Duration = limit.Duration * time.Second

			for _, remoteEntityScenario := range h.uclpp.RemoteEntitiesScenarios() {
				h.sendProductionLimit(remoteEntityScenario.Entity)
			}
		case SetConsumptionFailsafeValue:
			var limit = data.Value

			h.consumptionFailsafeLimits.Value = limit

			for _, remoteEntityScenario := range h.uclpc.RemoteEntitiesScenarios() {
				h.sendConsumptionFailsafeLimit(remoteEntityScenario.Entity)
			}
		case SetConsumptionFailsafeDuration:
			var limit = data.Value

			h.consumptionFailsafeLimits.Duration = time.Duration(limit) * time.Second

			for _, remoteEntityScenario := range h.uclpc.RemoteEntitiesScenarios() {
				h.sendConsumptionFailsafeDuration(remoteEntityScenario.Entity)
			}
		case SetProductionFailsafeValue:
			var limit = data.Value

			h.productionFailsafeLimits.Value = limit

			for _, remoteEntityScenario := range h.uclpp.RemoteEntitiesScenarios() {
				h.sendProductionFailsafeLimit(remoteEntityScenario.Entity)
			}
		case SetProductionFailsafeDuration:
			var limit = data.Value

			h.productionFailsafeLimits.Duration = time.Duration(limit) * time.Second

			for _, remoteEntityScenario := range h.uclpp.RemoteEntitiesScenarios() {
				h.sendProductionFailsafeDuration(remoteEntityScenario.Entity)
			}
		case StopConsumptionHeartbeat:
			h.uclpc.StopHeartbeat()
		case StartConsumptionHeartbeat:
			h.uclpc.StartHeartbeat()
		}

		frontend.sendNotification(Acknowledge)
	}
}
