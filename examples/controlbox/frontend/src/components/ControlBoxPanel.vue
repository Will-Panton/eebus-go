<template>
  <h1>Control Box Simulator</h1>
  <div class="header-frame">
    <div class="header" v-if="'' < qrcode">
      <label class="qrcode-text">{{ qrcode }}</label>
      <qrcode-vue  class="qrcode" :value="qrcode" :size="130" level="H" render-as="svg" />
    </div>
  </div>
  <div v-if="'' == qrcode">
    <h3>not running</h3>
  </div>
  <div v-else>
    <div v-if="0 < remoteServices?.length" class="devices">
      <label class="device-select-label">Remote Device:</label>
      <VueSelect v-model="selectedSki" :options="optionServices"
        placeholder="Select a device" @option-selected="serviceSelected">
      </VueSelect>
    </div>
    <div v-else>
      <h3>No devices found</h3>
    </div>

    <div v-if="'' < selectedSki" class="devices">
      <label class="device-select-label">SKI:</label>
      <label class="device-select-label">{{ selectedSki }}</label>
    </div>

    <div v-if="'' < selectedSki && !! remoteEntities" class="devices">
      <label class="device-select-label">Entities:</label>
      <VueSelect v-model="selectedEntity" :options="optionEntities"
        v-bind:placeholder="optionEntities.length + (optionEntities.length == 1 ? ' entity' : ' entities')">
      </VueSelect>
      <label class="device-select-label">Device Type:</label>
      <label class="device-select-label">{{ deviceType }}</label>
      <label class="device-select-label">Features:</label>
      <VueSelect :options="optionFeatures"
        v-bind:placeholder="optionFeatures.length == 0 ? '' : (optionFeatures.length + (optionFeatures.length == 1 ? ' feature' : ' features'))">
      </VueSelect>
    </div>
    <div class="usecases">
      <div v-if="'' < selectedSki && !!selectedDd && !!selectedDd['LPC']">
        <h3>Consumption Limit</h3>
        <div class="form-line">
          <label>Active:</label>
          <input type="checkbox" v-model="selectedDd['LPC'].IsActive"/>

          <label>Dimmed Value [W]:</label>
          <input type="number" v-model="selectedDd['LPC'].Value" />
          <button class="three-lines" type="button" @click="setConsumptionLimit">Set</button>

          <label>Dimmed Duration [s]:</label>
          <input type="number" v-model="selectedDd['LPC'].Duration" />

          <label>Failsafe Value [W]:</label>
          <input type="number" v-model="selectedDd['LPC'].FSValue" />
          <button type="button" @click="setConsumptionFailsafeLimit">Set</button>

          <label>Failsafe Duration [s]:</label>
          <input type="number" v-model="selectedDd['LPC'].FSDuration" />
          <button type="button" @click="setConsumptionFailsafeDuration">Set</button>

          <label>Nominal Maximum [W]:</label>
          <input type="number" v-model="consumptionNominalMax" />
          <div></div>
          <!-- <button type="button" @click="getConsumptionNominalMax">Get</button> -->

          <label>Heartbeat:</label>
          <span v-bind:class = "(consumptionHeartbeat)?'pulse heartbeat':'pulse'">&#9673;</span>
          <!-- <button type="button" @click="toggleConsumptionHeartbeat">{{ consumptionHeartbeatEnabled ? 'Stop' : 'Start' }}</button> -->
          <div></div>
        </div>
      </div>
      <div v-if="'' < selectedSki && !!selectedDd && !!selectedDd['LPP']">
        <h3>Production Limit</h3>
        <div class="form-line">
          <label>Active:</label>
          <input type="checkbox" v-model="selectedDd['LPP'].IsActive"/>

          <label>Dimmed Value [W]:</label>
          <input type="number" v-model="selectedDd['LPP'].Value" />
          <button class="three-lines" type="button" @click="setProductionLimit">Set</button>
          
          <label>Dimmed Duration [s]:</label>
          <input type="number" v-model="selectedDd['LPP'].Duration" />
          
          <label>Failsafe Value [W]:</label>
          <input type="number" v-model="selectedDd['LPP'].FSValue" />
          <button type="button" @click="setProductionFailsafeLimit">Set</button>
          
          <label>Failsafe Duration [s]:</label>
          <input type="number" v-model="selectedDd['LPP'].FSDuration" />
          <button type="button" @click="setProductionFailsafeDuration">Set</button>
          
          <label>Nominal Maximum [W]:</label>
          <input type="number" v-model="productionNominalMax" />
          <div></div>

          <label>Heartbeat:</label>
          <span v-bind:class = "(productionHeartbeat)?'pulse heartbeat':'pulse'">&#9673;</span>
          <!-- <button type="button" @click="toggleProductionHeartbeat">{{ productionHeartbeatEnabled ? 'Stop' : 'Start' }}</button> -->
          <div></div>
        </div>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
  import { Component, Vue, toNative } from 'vue-facing-decorator'
  import QrcodeVue from 'qrcode.vue'
  import VueSelect from 'vue3-select-component'
  //import { reactive } from 'vue'

  enum MessageType {
    Text                           = 0,
    QRCode                         = 1,
    Acknowledge                    = 2,
    ServiceListChanged             = 3,
    GetServiceList                 = 4,
    SelectService                  = 5,
    GetEntityInfo                  = 6,
    GetAllData                     = 7,
    SetConsumptionLimit            = 8,
    GetConsumptionLimit            = 9,
    SetProductionLimit             = 10,
    GetProductionLimit             = 11,
    SetConsumptionFailsafeValue    = 12,
    GetConsumptionFailsafeValue    = 13,
    SetConsumptionFailsafeDuration = 14,
    GetConsumptionFailsafeDuration = 15,
    SetProductionFailsafeValue     = 16,
    GetProductionFailsafeValue     = 17,
    SetProductionFailsafeDuration  = 18,
    GetProductionFailsafeDuration  = 19,
    GetConsumptionNominalMax       = 20,
    GetProductionNominalMax        = 21,
    GetConsumptionHeartbeat        = 22,
    StopConsumptionHeartbeat       = 23,
    StartConsumptionHeartbeat      = 24,
    GetProductionHeartbeat         = 25,
    StopProductionHeartbeat        = 26,
    StartProductionHeartbeat       = 27,
  }

  interface Limits {
    IsActive:   boolean,
	  Value:      number,
	  Duration:   number,
	  FSValue:    number,
	  FSDuration: number
  }

  interface RemoteService {
    name:       string,
	  ski:        string,
	  identifier: string,
	  brand:      string,
	  type:       string,
	  model:      string,
	  serial:     string,
	  categories: number[]
  }

  interface EntityInfo {
    Name:     string,
    SKI:      string,
    Type:     string,
    Features: string[],
    UseCases: string[]
  }

  interface Message {
    Type:         MessageType,
    Text?:        string,
    Limit?:       Limits,
    Value?:       number,
    ServiceList?: RemoteService[],
    EntityInfos?: EntityInfo[],
    UseCase?:     string
  }

  type UCLimits = {[key:string]:Limits};
  type DeviceData = {[key:string]:UCLimits};

  @Component({
    components: {
      QrcodeVue,
      VueSelect
    }
  })
  export class ControlBoxPanel extends Vue {
    public qrcode = "";

    public dd: DeviceData = {};

    public remoteServices: RemoteService[] = [];
    public remoteEntities: EntityInfo[] | undefined = [];
    public selectedSki = "";
    public selectedEntity: EntityInfo | undefined = undefined;

    public get selectedDd() {
      return this.dd[this.selectedSki];
    }

    public get optionServices() {
      var options:any[] = [];
      this.remoteServices.forEach(item => { options.push({
          label: item.brand + " " + item.model + ("" < item.serial ? (", SN-" + item.serial) : ""),
          value: item.ski
        });        
      });
      return options;
    }

    public get selectedEntities(): EntityInfo[] {
      if ( "" < this.selectedSki && !! this.remoteEntities ) {
        return this.remoteEntities.filter( (re) => re.SKI == this.selectedSki );
      }
      else {
        return [];
      }
    }

    public get optionEntities() {
      var options:any[] = [];

      if ( "" < this.selectedSki && !! this.selectedEntities ) {
        this.selectedEntities.forEach(item => { options.push({
            label: item.Name,
            value: item
          });        
        });
      }
      return options;
    }

    public get deviceType() {
      if ( "" < this.selectedSki && !! this.selectedEntity ) {
        return this.selectedEntity.Type;
      }
      else {
        return "";
      }
    }

    public get optionFeatures() {
      var options:any[] = [];

      if ( "" < this.selectedSki && !! this.selectedEntity ) {
        this.selectedEntity.Features.forEach(item => { options.push({
            label: item,
            value: item
          });        
        });
      }
      return options;
    }

    public consumptionNominalMax: number = 0;
    public productionNominalMax:  number = 0;

    public consumptionHeartbeat:        boolean = false;
    public consumptionHeartbeatEnabled: boolean = true;
    public productionHeartbeat:         boolean = false;
    public productionHeartbeatEnabled:  boolean = true;

    private socket: WebSocket | undefined;
  
    mounted() {
      this.socket = new WebSocket( "ws://" + window.location.hostname + ":7080/ws" );
      console.log( "Attempting Connection..." );

      this.socket.onopen = () => {
          console.log( "Successfully Connected" );
          this.sendNotification( MessageType.GetEntityInfo );
      };
      
      this.socket.onclose = event => {
          console.log( "Socket Closed Connection: ", event );
          this.sendText( "Client Closed!" );
          this.socket = undefined;
      };

      this.socket.onerror = error => {
          console.log( "Socket Error: ", error );
      };

      this.socket.onmessage = event => {
        console.log( "Socket message: ", event.data );
        var message: Message = JSON.parse( event.data );
        switch ( message.Type ) {
          case MessageType.QRCode: {
            this.qrcode = message.Text as string;
            console.log( "SHIPID: ", this.qrcode );
            break;
          }
          case MessageType.ServiceListChanged: {
            this.sendNotification( MessageType.GetServiceList );
            break;
          }
          case MessageType.GetServiceList: {
            this.remoteServices = message.ServiceList!;
            break;
          }
          case MessageType.GetEntityInfo: {
            this.remoteEntities = message.EntityInfos;
            break;
          }
          case MessageType.GetConsumptionLimit:
          case MessageType.GetProductionLimit: {
            this.updateDeviceData( message.UseCase! );
            this.dd[this.selectedSki][message.UseCase!].IsActive = message.Limit?.IsActive ?? false;
            this.dd[this.selectedSki][message.UseCase!].Value    = message.Limit?.Value ?? 0;
            this.dd[this.selectedSki][message.UseCase!].Duration = message.Limit?.Duration ?? 0;
            break;
          }
          case MessageType.GetConsumptionFailsafeValue:
          case MessageType.GetProductionFailsafeValue: {
            this.updateDeviceData( message.UseCase! );
            this.dd[this.selectedSki][message.UseCase!].FSValue = message.Value ?? 0;
            break;
          }
          case MessageType.GetConsumptionFailsafeDuration:
          case MessageType.GetProductionFailsafeDuration: {
            this.updateDeviceData( message.UseCase! );
            this.dd[this.selectedSki][message.UseCase!].FSDuration = message.Value ?? 0;
            break;
          }
          case MessageType.GetConsumptionNominalMax: {
            this.consumptionNominalMax = message.Value ?? 0;
            break;
          }
          case MessageType.GetProductionNominalMax: {
            this.productionNominalMax = message.Value ?? 0;
            break;
          }
          case MessageType.GetConsumptionHeartbeat: {
            this.consumptionHeartbeat = false;
            setTimeout( () => this.consumptionHeartbeat = true, 1 );
            break;
          }
          case MessageType.GetProductionHeartbeat: {
            this.productionHeartbeat = false;
            setTimeout( () => this.productionHeartbeat = true, 1 );
            break;
          }
        }   
      }
    }

    private updateDeviceData( useCase: string ) {
      if ( ! this.dd[this.selectedSki] )
          this.dd[this.selectedSki] = {};
      if ( ! this.dd[this.selectedSki][useCase] )
          this.dd[this.selectedSki][useCase] = {} as Limits;
    }

    public serviceSelected() {
      this.selectedEntity = undefined;
      this.sendNotification( MessageType.SelectService, this.selectedSki );
    }

    private sendNotification( type: MessageType, param: string = "" ) {
      let command: Message = {
        Type: type,
        Text: param
      };

      this.socket!.send( JSON.stringify( command ) );
    }

    private sendText( text: string ) {
      let command: Message = {
        Type: MessageType.Text,
        Text: text,
      };

      this.socket!.send( JSON.stringify( command ) );
    }

    private sendLimits( type: MessageType, value: Limits ) {
      let command: Message = {
        Type:  type,
        Limit: value
      };

      this.socket!.send( JSON.stringify( command ) );
    }

    private sendValue( type: MessageType, value: number ) {
      let command: Message = {
        Type:  type,
        Value: value
      };

      this.socket!.send( JSON.stringify( command ) );
    }

    public setConsumptionLimit() {
      if ( ! this.socket )
        return;
      
      this.sendLimits( MessageType.SetConsumptionLimit, this.dd[this.selectedSki]['LPC'] );
    }

    public setProductionLimit() {
      if ( ! this.socket )
        return;
      
      this.sendLimits( MessageType.SetProductionLimit, this.dd[this.selectedSki]['LPP'] );
    }

    public setConsumptionFailsafeLimit() {
      if ( ! this.socket )
        return;
      
      this.sendValue( MessageType.SetConsumptionFailsafeValue, this.dd[this.selectedSki]['LPC'].FSValue );
    }

    public setConsumptionFailsafeDuration() {
      if ( ! this.socket )
        return;
      
      this.sendValue( MessageType.SetConsumptionFailsafeDuration, this.dd[this.selectedSki]['LPC'].FSDuration );
    }

    public setProductionFailsafeLimit() {
      if ( ! this.socket )
        return;
      
      this.sendValue( MessageType.SetProductionFailsafeValue, this.dd[this.selectedSki]['LPP'].FSValue );
    }

    public setProductionFailsafeDuration() {
      if ( ! this.socket )
        return;
      
      this.sendValue( MessageType.SetProductionFailsafeDuration, this.dd[this.selectedSki]['LPP'].FSDuration );
    }

    // public getConsumptionNominalMax() {
    //   if ( ! this.socket )
    //     return;
      
    //   this.sendValue( MessageType.GetConsumptionNominalMax, 0 );
    // }

    public toggleConsumptionHeartbeat() {
      if ( ! this.socket )
        return;

      if ( this.consumptionHeartbeatEnabled )
        this.sendValue( MessageType.StopConsumptionHeartbeat, 0 );
      else
        this.sendValue( MessageType.StartConsumptionHeartbeat, 0 );

      this.consumptionHeartbeatEnabled = ! this.consumptionHeartbeatEnabled;
    }

    public toggleProductionHeartbeat() {
      if ( ! this.socket )
        return;

      if ( this.productionHeartbeatEnabled )
        this.sendValue( MessageType.StopProductionHeartbeat, 0 );
      else
        this.sendValue( MessageType.StartProductionHeartbeat, 0 );

      this.productionHeartbeatEnabled = ! this.productionHeartbeatEnabled;
    }
  }

  export default toNative( ControlBoxPanel )
</script>

<style scoped>
  .read-the-docs {
    color: #888;
  }
  .header-frame {
    display: inline-table;
    margin-bottom: 10px;
  }
  .header {
    display: grid;
    grid-template-columns: 75fr 25fr;
    column-gap: 25px;
  }
  h1 {
    margin-top: 0.1em;
  }
  .qrcode-text {
    width: 370px;
    text-align: left;
    word-break: break-all;
  }
  .qrcode {
    width: 130px;
    height: 100%;
  }
  .usecases {
    display: grid;
    grid-template-columns: 50fr 50fr;
    column-gap: 25px;
  }
  .three-lines {
    grid-column-start: 3;
    grid-row-start: 1;
    grid-row-end: 4;
  }
  .form-line {
    display: grid;
    grid-template-columns: 50fr 30fr 20fr;
    column-gap: 10px;
  }
  .form-line label {
    align-content: center;
    text-align: left;
  }
  .form-line input {
    font-size: initial;
    width: 100px;
    align-self: center;
  }
  .form-line button {
    line-height: 5px;
    height: 100%;
  }
  
  .pulse {
    font-size: 25px;
  }

  .heartbeat {
    animation-name: heartbeat;
    animation-duration: 1s;
    /* animation-iteration-count: infinite; */
  }

  @keyframes heartbeat {
    from {
      color: rgb(0,255,0);
    }

    25% {
      color: rgb(0,255,0);
    }

    50% {
      color: rgb(0,127,0);
    }

    75% {
      color: rgb(0,63,0);
    }

    to {
      color: rgb(0,0,0);
    }
  }

  .devices {
    display: grid;
    grid-template-columns: 20fr 80fr;
    column-gap: 10px;
  }
  .device-select-label {
    text-align: left;
    line-height: 2.2em;
  }
</style>
