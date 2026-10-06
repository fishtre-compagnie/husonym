// Package piidetect runs the PII detection jobs: a run lists the tables of the source of
// its job and says, column by column, which hold personal data. It writes nothing to the
// source; its product is a report per table and an index per run, stored in the run
// contexts of the API (package report).
//
// The workflow JobPiiDetect is a run of a job. It scans each table in a child workflow,
// TablePiiDetect, a fixed number of them at once. A table is scanned by up to three
// detections whose findings are stored side by side: rules on the names and on the
// formats of the values (package rules), a language model, when the deployment configures
// one (package model), and a content analyzer, when the API has one. When the job samples
// data, 200 rows of the table are read and reduced to a profile per column (package
// profile); the rows do not leave the activity that read them. The model is given names,
// types and profiles, and sample values only for a job that asks for them. The analyzer
// is given the values of the free-text columns the rules found nothing in, by the API:
// the worker names the columns, the API reads their values from the source and sends them
// to the analyzer, and returns counts per column. Nothing of the values reaches the
// worker. A table that learns that the API has no analyzer makes the run tell it to the
// tables it starts afterwards, which do not ask.
//
// What is recorded in the histories of the runs, or stored, must stay as it is for the
// runs to continue and to replay, and for the stored reports to be read:
//
//   - the registered names, which are the names of the functions: JobPiiDetect,
//     TablePiiDetect, GetPiiDetectJobDetails, GetLastSuccessfulWorkflowId,
//     GetTablesToPiiScan, SaveJobPiiDetectReport, GetColumnData, DetectPiiRegex,
//     DetectPiiLLM, DetectPiiContent, SaveTablePiiDetectReport. The API starts
//     JobPiiDetect by its name and decodes the input of TablePiiDetect from the history
//     of a run. A run also asks for RecordRunStarted and RecordRunEnded, of the package
//     runusage;
//   - the serialized form of the requests and responses, whose keys are the names of
//     their Go fields. A member is only ever added, and left out when it is empty: a
//     run replays to the result it recorded, which holds none of the later members;
//   - the commands of JobPiiDetect: the license read, the report of its start to the
//     API, GetPiiDetectJobDetails, the account hooks of the start,
//     GetLastSuccessfulWorkflowId for an incremental job, GetTablesToPiiScan, the
//     children as scanTables arranges them, with their ids, SaveJobPiiDetectReport, the
//     account hooks of the end, the report of its end to the API. A run started before
//     it reported its start and its end has neither of the two reports;
//   - the commands of TablePiiDetect: GetColumnData, DetectPiiRegex, DetectPiiLLM,
//     DetectPiiContent, SaveTablePiiDetectReport, one after the other. DetectPiiContent
//     is scheduled only for a table that has free-text columns the rules found nothing
//     in, under the change id "pii-detect-content-analysis", and not in a run of a table
//     that was told that the API has no analyzer;
//   - the keys and the JSON of the stored reports.
//
// Adding, removing or reordering an activity, a child, a timer or a side effect needs
// workflow.GetVersion: see versions.go. What the activities do, how long they may run
// and how they are retried is not compared on replay, nor is what an activity or a child
// is given: a run replays with the answers it recorded. The workflows read no setting of
// the process: what a run depends on is in its history.
package piidetect
