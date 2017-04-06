package mqtt

import (
	"context"
	"encoding/json"
	"github.com/TIBCOSoftware/flogo-lib/core/action"
	"github.com/TIBCOSoftware/flogo-lib/core/trigger"
	"github.com/TIBCOSoftware/flogo-lib/flow/support"
	"github.com/TIBCOSoftware/flogo-lib/types"
	"github.com/eclipse/paho.mqtt.golang"
	"strconv"
	"github.com/TIBCOSoftware/flogo-lib/logger"
)

// log is the default package logger
var log = logger.GetLogger("trigger-tibco-mqtt")

var md = trigger.NewMetadata(jsonMetadata)

// todo: switch to use endpoint registration

// MqttTrigger is simple MQTT trigger
type MqttTrigger struct {
	metadata          *trigger.Metadata
	runner            action.Runner
	client            mqtt.Client
	config            types.TriggerConfig
	topicToActionURI  map[string]string
	topicToActionType map[string]string
	instanceId        string
}

type MQTTFactory struct {
}

func (t *MQTTFactory) New(id string) trigger.Trigger2 {
	return &MqttTrigger{metadata: md, instanceId: id}
}
func init() {
	trigger.RegisterFactory(md.ID, &MQTTFactory{})
}

// Metadata implements trigger.Trigger.Metadata
func (t *MqttTrigger) Metadata() *trigger.Metadata {
	return t.metadata
}

// Init implements ext.Trigger.Init
func (t *MqttTrigger) Init(config types.TriggerConfig, runner action.Runner) {
	t.config = config
	t.runner = runner
}

// Start implements ext.Trigger.Start
func (t *MqttTrigger) Start() error {

	opts := mqtt.NewClientOptions()
	opts.AddBroker(t.config.Settings["broker"].(string))
	opts.SetClientID(t.config.Settings["id"].(string))
	opts.SetUsername(t.config.Settings["user"].(string))
	opts.SetPassword(t.config.Settings["password"].(string))
	b, err := strconv.ParseBool(t.config.Settings["cleansess"].(string))
	if err != nil {
		log.Error("Error converting \"cleansess\" to a boolean ", err.Error())
		return err
	}
	opts.SetCleanSession(b)
	if storeType := t.config.Settings["store"].(string); storeType != ":memory:" {
		opts.SetStore(mqtt.NewFileStore(t.config.Settings["store"].(string)))
	}

	opts.SetDefaultPublishHandler(func(client mqtt.Client, msg mqtt.Message) {
		topic := msg.Topic()
		//TODO we should handle other types, since mqtt message format are data-agnostic
		payload := string(msg.Payload())
		log.Debug("Received msg:", payload)
		actionURI, found := t.topicToActionURI[topic]
		if found {
			t.RunAction(actionURI, payload)
		} else {
			log.Errorf("Topic %s not found", t.topicToActionURI[topic])
		}
	})

	client := mqtt.NewClient(opts)
	t.client = client
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	i, err := strconv.Atoi(t.config.Settings["qos"].(string))
	if err != nil {
		log.Error("Error converting \"qos\" to an integer ", err.Error())
		return err
	}

	t.topicToActionType = make(map[string]string)
	t.topicToActionURI = make(map[string]string)

	for _, endpoint := range t.config.Handlers {
		if token := t.client.Subscribe(endpoint.Settings["topic"].(string), byte(i), nil); token.Wait() && token.Error() != nil {
			log.Errorf("Error subscribing to topic %s: %s", endpoint.Settings["topic"], token.Error())
			panic(token.Error())
		} else {
			t.topicToActionURI[endpoint.Settings["topic"].(string)] = endpoint.ActionId
		}
	}

	return nil
}

// Stop implements ext.Trigger.Stop
func (t *MqttTrigger) Stop() error {
	//unsubscribe from topic
	log.Debug("Unsubcribing from topic: ", t.config.Settings["topic"].(string))
	for _, endpoint := range t.config.Handlers {
		if token := t.client.Unsubscribe(endpoint.Settings["topic"].(string)); token.Wait() && token.Error() != nil {
			log.Errorf("Error unsubscribing from topic %s: %s", endpoint.Settings["topic"].(string), token.Error())
		}
	}

	t.client.Disconnect(250)

	return nil
}

// RunAction starts a new Process Instance
func (t *MqttTrigger) RunAction(actionURI string, payload string) {

	req := t.constructStartRequest(payload)
	//err := json.NewDecoder(strings.NewReader(payload)).Decode(req)
	//if err != nil {
	//	//http.Error(w, err.Error(), http.StatusBadRequest)
	//	log.Error("Error Starting action ", err.Error())
	//	return
	//}

	//todo handle error
	startAttrs, _ := t.metadata.OutputsToAttrs(req.Data, false)

	action := action.Get2(actionURI)
	context := trigger.NewContext(context.Background(), startAttrs)
	_, replyData, err := t.runner.Run(context, action, actionURI, nil)
	if err != nil {
		log.Error("Error starting action: ", err.Error())
	}
	log.Debugf("Ran action: [%s]", actionURI)

	if replyData != nil {
		data, err := json.Marshal(replyData)
		if err != nil {
			log.Error(err)
		} else {
			t.publishMessage(req.ReplyTo, string(data))
		}
	}
}

func (t *MqttTrigger) publishMessage(topic string, message string) {

	log.Debug("ReplyTo topic: ", topic)
	log.Debug("Publishing message: ", message)

	qos, err := strconv.Atoi(t.config.Settings["qos"].(string))
	if err != nil {
		log.Error("Error converting \"qos\" to an integer ", err.Error())
		return
	}
	token := t.client.Publish(topic, byte(qos), false, message)
	token.Wait()
}

func (t *MqttTrigger) constructStartRequest(message string) *StartRequest {
	//TODO how to handle reply to, reply feature
	req := &StartRequest{}
	data := make(map[string]interface{})
	data["message"] = message
	req.Data = data
	return req
}

// StartRequest describes a request for starting a ProcessInstance
type StartRequest struct {
	ProcessURI  string                 `json:"flowUri"`
	Data        map[string]interface{} `json:"data"`
	Interceptor *support.Interceptor   `json:"interceptor"`
	Patch       *support.Patch         `json:"patch"`
	ReplyTo     string                 `json:"replyTo"`
}
