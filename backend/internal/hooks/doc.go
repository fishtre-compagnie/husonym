// Package hooks holds the two kinds of hook an account configures, and decides who may do
// what with them.
//
// A job hook is SQL attached to a job, which a run executes on one of the job's connections
// before or after the sync. An account hook is a webhook attached to an account, called when
// a run of the account is created, succeeds or fails. The package stores, validates and
// returns their definitions; running them is the worker's.
//
// Every operation checks its caller in one order, in one place (admit):
//
//  1. the owner of what the request names is found: the account of the job, of the hook;
//  2. the caller sees that owner: by viewing it, or by belonging to its account and holding
//     all that the operation asks — otherwise the answer is the one given for what does not
//     exist, so that nothing is learnt of it;
//  3. the caller holds each permission the operation asks;
//  4. the object does not refuse the operation: a kind that is retired is neither created
//     nor turned on;
//  5. the account holds a license, where the operation creates, changes or turns on.
//
// What the request carries is validated after these, and then written. What each operation
// asks is data (rules), one entry per procedure of the contract.
package hooks
