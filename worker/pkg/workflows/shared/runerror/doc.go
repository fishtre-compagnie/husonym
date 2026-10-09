// Package runerror says in which category the error of a run falls, for the usage report of
// the run. The categories are a closed list, mgmtv1alpha1.RunErrorCategory.
//
// A category comes from the Go type of an error and from the code of the database, SQLSTATE
// for PostgreSQL and the error number for MySQL. It never comes from the text: nothing here
// asks an error for its message, and what is recognized by neither its type nor its code is
// "other". Test_ThePackage_NeverAsksAnErrorForItsText holds the source to it.
//
// An error is typed in the worker only. A workflow sees of a failed activity what Temporal
// recorded: a message, a type name and causes, in which a database error is no longer one.
// So the error is classified where it is still typed, by an interceptor around every
// activity (NewInterceptor), and its category leaves the activity in the details of the
// application error of Temporal, as the one value {"RunErrorCategory": <number>} (Carry).
// The workflow reads it back with CategoryOf.
//
// An activity that makes an error whose category no type tells gives it the category itself,
// the same way (Tell): a refusal of the license (License), a pre-flight check that stops the
// run.
//
// The interceptor observes. What Temporal records of the error, apart from the details, and
// what the worker reads of it to retry or to cancel the activity, are what they were without
// it; an error that cannot be given details at that price is left as it is, and so is the
// error whose inspection panics. A workflow is not intercepted, and reading details is a
// pure function of the failure: the recorded histories replay.
//
// What must stay as it is: the name of the member RunErrorCategory, which is written in the
// histories of the runs, and the numbers of the enum, which are what it holds.
package runerror
