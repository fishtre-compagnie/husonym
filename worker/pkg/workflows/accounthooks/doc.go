// Package accounthooks tells the hooks of an account about the lifecycle events of its job
// runs: created, failed, succeeded.
//
// The workflow ProcessAccountHook handles one event. It asks the API which hooks of the
// account listen to the event, then executes each of them in an activity of its own; a
// webhook is delivered by the webhook package. The root workflows of a job run start it as
// a child that outlives them and never read its result: a hook does not change a run.
//
// Three things are recorded in the histories of the runs and must stay as they are for
// those runs to continue and to replay:
//
//   - the registered names, which are the names of the functions: ProcessAccountHook,
//     GetAccountHooksByEvent, ExecuteAccountHook;
//   - the serialized form of the requests and responses, whose keys are the names of
//     their Go fields;
//   - the commands of the workflow: one lookup, then one execution per hook, all scheduled
//     in the order of the lookup before the first one is awaited. Adding, removing or
//     reordering an activity, a timer or a side effect needs workflow.GetVersion.
//
// What the activities do, how long they may run and how they are retried is not compared
// on replay.
package accounthooks
