package flow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"

	"github.com/TIBCOSoftware/flogo-contrib/action/flow/definition"
	"github.com/TIBCOSoftware/flogo-contrib/action/flow/instance"
	"github.com/TIBCOSoftware/flogo-contrib/action/flow/model"
	_ "github.com/TIBCOSoftware/flogo-contrib/action/flow/model/simple"
	"github.com/TIBCOSoftware/flogo-contrib/action/flow/support"
	"github.com/TIBCOSoftware/flogo-lib/core/action"
	"github.com/TIBCOSoftware/flogo-lib/core/data"
	"github.com/TIBCOSoftware/flogo-lib/core/mapper"
	"github.com/TIBCOSoftware/flogo-lib/logger"
	"github.com/TIBCOSoftware/flogo-lib/util"
)

const (
	FLOW_REF = "github.com/TIBCOSoftware/flogo-contrib/action/flow/ondemand"

	ENV_FLOW_RECORD = "FLOGO_FLOW_RECORD"

	ivFlowPackage = "flowPackage"
)

type FlowAction struct {
	flowURI    string
	ioMetadata *data.IOMetadata
}

//type ActionData struct {
//	// The flow is a URI
//	FlowURI string `json:"flowURI"`
//
//	// The flow is embedded and uncompressed
//	//DEPRECATED
//	Flow json.RawMessage `json:"flow"`
//
//	// The flow is a URI
//	//DEPRECATED
//	FlowCompressed json.RawMessage `json:"flowCompressed"`
//}

//var ep ExtensionProvider
var idGenerator *util.Generator
var record bool
var manager *support.FlowManager

//todo expose and support this properly
var maxStepCount = 1000000

//todo fix this
var metadata = &action.Metadata{ID: "github.com/TIBCOSoftware/flogo-contrib/action/flow", Async: true}

func init() {
	action.RegisterFactory(FLOW_REF, &ActionFactory{})
}

//func SetExtensionProvider(provider ExtensionProvider) {
//	ep = provider
//}

type ActionFactory struct {
}

func (ff *ActionFactory) Init() error {

	//if manager != nil {
	//	return nil
	//}
	//
	//if ep == nil {
	//	testerEnabled := os.Getenv(tester.ENV_ENABLED)
	//	if strings.ToLower(testerEnabled) == "true" {
	//		ep = tester.NewExtensionProvider()
	//
	//		sm := util.GetDefaultServiceManager()
	//		sm.RegisterService(ep.GetFlowTester())
	//		record = true
	//	} else {
	//		ep = NewDefaultExtensionProvider()
	//		record = recordFlows()
	//	}
	//}
	//
	//definition.SetMapperFactory(ep.GetMapperFactory())
	//definition.SetLinkExprManagerFactory(ep.GetLinkExprManagerFactory())
	//
	//if idGenerator == nil {
	//	idGenerator, _ = util.NewGenerator()
	//}
	//
	//model.RegisterDefault(ep.GetDefaultFlowModel())
	//manager = support.NewFlowManager(ep.GetFlowProvider())
	//resource.RegisterManager(support.RESTYPE_FLOW, manager)

	return nil
}

func recordFlows() bool {
	recordFlows := os.Getenv(ENV_FLOW_RECORD)
	if len(recordFlows) == 0 {
		return false
	}
	b, _ := strconv.ParseBool(recordFlows)
	return b
}

func GetFlowManager() *support.FlowManager {
	return manager
}

func (ff *ActionFactory) New(config *action.Config) (action.Action, error) {

	flowAction := &FlowAction{}

	//temporary hack to support dynamic process running by tester
	if config.Data == nil {
		return flowAction, nil
	}

	//var actionData ActionData
	//err := json.Unmarshal(config.Data, &actionData)
	//if err != nil {
	//	return nil, fmt.Errorf("faild to load flow action data '%s' error '%s'", config.Id, err.Error())
	//}
	//
	//if len(actionData.FlowURI) > 0 {
	//
	//	flowAction.flowURI = actionData.FlowURI
	//} else {
	//	uri, err := createResource(&actionData)
	//	if err != nil {
	//		return nil, err
	//	}
	//	flowAction.flowURI = uri
	//}
	//
	//if config.Metadata != nil {
	//	flowAction.ioMetadata = config.Metadata
	//} else {
	//	//todo add flag to remove startup validation
	//	def, err := manager.GetFlow(flowAction.flowURI)
	//	if err != nil {
	//		return nil, err
	//	} else {
	//		if def == nil {
	//			return nil, errors.New("unable to resolve flow: " + flowAction.flowURI)
	//		}
	//	}
	//
	//	flowAction.ioMetadata = def.Metadata()
	//}

	return flowAction, nil
}

//Metadata get the Action's metadata
func (fa *FlowAction) Metadata() *action.Metadata {
	return metadata
}

func (fa *FlowAction) IOMetadata() *data.IOMetadata {
	return fa.ioMetadata
}

// Run implements action.Action.Run
func (fa *FlowAction) Run(ctx context.Context, inputs map[string]*data.Attribute, handler action.ResultHandler) error {

	//maybe ability to dynamically register flows, to improve performance

	if logger.GetLogLevel() == logger.DebugLevel {
		logInputs(inputs)
	}

	var inputData []*data.Attribute

	for key, value := range inputs {
		if key != ivFlowPackage {
			inputData = append(inputData, value)
		}
	}

	fpAttr, exists := inputs[ivFlowPackage]
	var flowPackage *FlowPackage

	if exists {
		raw := fpAttr.Value().(json.RawMessage)
		err := json.Unmarshal(raw, flowPackage)
		if err != nil {
			return err
		}
	} else {
		return errors.New("flow package not provided")
	}

	logger.Debugf("InputMappings: %+v", flowPackage.InputMappings)
	logger.Debugf("OutputMappings: %+v", flowPackage.OutputMappings)

	flowInputs, err := ApplyMappings(flowPackage.InputMappings, inputData, flowPackage.Flow.Metadata().Input)
	if err != nil {
		return err
	}

	instanceID := idGenerator.NextAsString()
	logger.Debug("Creating Flow Instance: ", instanceID)

	inst := instance.NewIndependentInstance(instanceID, "", flowPackage.Flow)

	logger.Debugf("Executing Flow Instance: %s", inst.ID())

	inst.Start(flowInputs)

	stepCount := 0
	hasWork := true

	inst.SetResultHandler(handler)

	go func() {

		defer handler.Done()

		if !inst.FlowDefinition().ExplicitReply() {

			idAttr, _ := data.NewAttribute("id", data.TypeString, inst.ID())
			results := map[string]*data.Attribute{
				"id": idAttr,
			}

			//todo remove
			//if old {
			//	dataAttr, _ := data.NewAttribute("data", data.OBJECT, &instance.IDResponse{ID: inst.ID()})
			//	results["data"] = dataAttr
			//	codeAttr, _ := data.NewAttribute("code", data.INTEGER, 200)
			//	results["code"] = codeAttr
			//}

			handler.HandleResult(results, nil)
		}

		for hasWork && inst.Status() < model.FlowStatusCompleted && stepCount < maxStepCount {
			stepCount++
			logger.Debugf("Step: %d", stepCount)
			hasWork = inst.DoStep()

			if record {
				//ep.GetStateRecorder().RecordSnapshot(inst)
				//ep.GetStateRecorder().RecordStep(inst)
			}
		}

		if inst.Status() == model.FlowStatusCompleted {
			returnData, err := inst.GetReturnData()

			//apply output mapper

			handler.HandleResult(returnData, err)
		} else if inst.Status() == model.FlowStatusFailed {
			handler.HandleResult(nil, inst.GetError())
		}

		logger.Debugf("Done Executing flow instance [%s] - Status: %d", inst.ID(), inst.Status())

		if inst.Status() == model.FlowStatusCompleted {
			logger.Infof("Flow instance [%s] Completed Successfully", inst.ID())
		} else if inst.Status() == model.FlowStatusFailed {
			logger.Infof("Flow instance [%s] Failed", inst.ID())
		}
	}()

	return nil
}

func logInputs(attrs map[string]*data.Attribute) {
	if len(attrs) > 0 {
		logger.Debug("Input Attributes:")
		for _, attr := range attrs {

			if attr == nil {
				logger.Error("Nil Attribute passed as input")
			} else {
				logger.Debugf(" Attr:%s, Type:%s, Value:%v", attr.Name(), attr.Type().String(), attr.Value())
			}
		}
	}
}

type FlowPackage struct {
	InputMappings  []interface{}
	OutputMappings []interface{}
	Flow           *definition.Definition
}

func ApplyMappings(mappings []interface{}, inputs []*data.Attribute, metadata map[string]*data.Attribute) (map[string]*data.Attribute, error) {

	mapperDef, err := mapper.NewMapperDefFromAnyArray(mappings)
	if err != nil {
		return nil, err
	}

	inScope := data.NewSimpleScope(inputs, nil)
	outScope := data.NewFixedScope(metadata)

	actionMapper := mapper.NewBasicMapper(mapperDef, data.GetBasicResolver())

	err = actionMapper.Apply(inScope, outScope)
	if err != nil {
		return nil, err
	}

	return outScope.GetAttrs(), nil
}
