package store_test

import (
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestServerlessStoreDeepCRUD(t *testing.T) {
	st := openTestStore(t)
	project := "p"
	loc := "us-central1"
	svcName := "projects/p/locations/us-central1/services/s1"
	created, err := st.CreateRunService(store.RunService{
		Name: svcName, ProjectID: project, Location: loc, ServiceID: "s1",
		TemplateJSON: `{"containers":[{"image":"demo"}]}`, LabResponseBody: `{"ok":true}`,
	})
	if err != nil || !created {
		t.Fatalf("create: %v %v", created, err)
	}
	svc, ok, err := st.GetRunService(svcName)
	if err != nil || !ok || svc.ServiceID != "s1" {
		t.Fatalf("get: %#v ok=%v err=%v", svc, ok, err)
	}
	list, err := st.ListRunServices(project, loc)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v", list)
	}
	updated, ok, err := st.UpdateRunService(svcName, `{"containers":[{"image":"v2"}]}`, `{"ok":2}`, "")
	if err != nil || !ok || updated.Generation < 2 {
		t.Fatalf("update: %#v ok=%v err=%v", updated, ok, err)
	}
	trafficked, ok, err := st.SetRunServiceTraffic(svcName, `[{"percent":100,"type":"TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST"}]`)
	if err != nil || !ok || trafficked.TrafficJSON == "" {
		t.Fatalf("traffic: %#v", trafficked)
	}
	if err := st.RecordRunInvoke(svcName, `{"status":200}`); err != nil {
		t.Fatal(err)
	}

	jobName := "projects/p/locations/us-central1/jobs/job1"
	created, err = st.CreateRunJob(store.RunJob{
		Name: jobName, ProjectID: project, Location: loc, JobID: "job1",
		TemplateJSON: `{"template":{}}`,
	})
	if err != nil || !created {
		t.Fatalf("create job: %v %v", created, err)
	}
	job, ok, err := st.GetRunJob(jobName)
	if err != nil || !ok {
		t.Fatal(err)
	}
	jobs, err := st.ListRunJobs(project, loc)
	if err != nil || len(jobs) != 1 || jobs[0].Name != job.Name {
		t.Fatalf("list jobs: %v", jobs)
	}
	uj, ok, err := st.UpdateRunJob(jobName, `{"template":{"v":2}}`)
	if err != nil || !ok || uj.Generation < 2 {
		t.Fatalf("update job: %#v", uj)
	}

	fnName := "projects/p/locations/us-central1/functions/f1"
	created, err = st.CreateCloudFunction(store.CloudFunction{
		Name: fnName, ProjectID: project, Location: loc, FunctionID: "f1",
		ConfigJSON: `{"buildConfig":{"source":{"storageSource":{"bucket":"b","object":"o.zip"}}}}`,
	})
	if err != nil || !created {
		t.Fatalf("create fn: %v %v", created, err)
	}
	fn, ok, err := st.GetCloudFunction(fnName)
	if err != nil || !ok {
		t.Fatal(err)
	}
	fns, err := st.ListCloudFunctions(project, loc)
	if err != nil || len(fns) != 1 || fns[0].Name != fn.Name {
		t.Fatalf("list fn: %v", fns)
	}
	ufn, ok, err := st.UpdateCloudFunction(fnName, `{"buildConfig":{}}`, `{"body":"x"}`)
	if err != nil || !ok || ufn.LabResponseJSON == "" {
		t.Fatalf("update fn: %#v", ufn)
	}
	sfn, ok, err := st.SetCloudFunctionState(fnName, "ACTIVE")
	if err != nil || !ok || sfn.State != "ACTIVE" {
		t.Fatalf("state: %#v", sfn)
	}
	if err := st.AcceptCloudFunctionUpload(store.CloudFunctionUpload{
		UploadID: "up1", ProjectID: project, Location: loc, Bucket: "b", Object: "o.zip",
	}); err != nil {
		t.Fatal(err)
	}
	up, ok, err := st.GetCloudFunctionUpload("up1")
	if err != nil || !ok || up.Bucket != "b" {
		t.Fatalf("get upload: %#v", up)
	}
	has, err := st.HasCloudFunctionUploadObject(project, "b", "o.zip")
	if err != nil || !has {
		t.Fatalf("has upload: %v %v", has, err)
	}
	n, err := st.ActivateCloudFunctionsForStorageSource(project, loc, "b", "o.zip")
	if err != nil {
		t.Fatal(err)
	}
	_ = n

	schedName := "projects/p/locations/us-central1/jobs/sched1"
	created, err = st.CreateSchedulerJob(store.SchedulerJob{
		Name: schedName, ProjectID: project, Location: loc, JobID: "sched1",
		Schedule: "*/5 * * * *", TimeZone: "UTC",
		HTTPTargetJSON: `{"uri":"http://127.0.0.1:4588/x"}`,
	})
	if err != nil || !created {
		t.Fatalf("sched create: %v %v", created, err)
	}
	sj, ok, err := st.GetSchedulerJob(schedName)
	if err != nil || !ok {
		t.Fatal(err)
	}
	sjs, err := st.ListSchedulerJobs(project, loc)
	if err != nil || len(sjs) != 1 {
		t.Fatalf("list sched: %v", sjs)
	}
	sj.Schedule = "0 * * * *"
	ok, err = st.UpdateSchedulerJob(sj)
	if err != nil || !ok {
		t.Fatalf("update sched: %v %v", ok, err)
	}
	if err := st.MarkSchedulerJobAttempt(schedName); err != nil {
		t.Fatal(err)
	}

	qName := "projects/p/locations/us-central1/queues/q1"
	created, err = st.CreateCloudTasksQueue(store.CloudTasksQueue{
		Name: qName, ProjectID: project, Location: loc, QueueID: "q1",
	})
	if err != nil || !created {
		t.Fatalf("queue: %v %v", created, err)
	}
	q, ok, err := st.GetCloudTasksQueue(qName)
	if err != nil || !ok {
		t.Fatal(err)
	}
	qs, err := st.ListCloudTasksQueues(project, loc)
	if err != nil || len(qs) != 1 || qs[0].Name != q.Name {
		t.Fatalf("list q: %v", qs)
	}
	q.State = "PAUSED"
	ok, err = st.UpdateCloudTasksQueue(q)
	if err != nil || !ok {
		t.Fatalf("update q: %v %v", ok, err)
	}
	taskName := qName + "/tasks/t1"
	created, err = st.CreateCloudTask(store.CloudTask{
		Name: taskName, QueueName: qName, HTTPRequestJSON: `{"url":"http://x"}`,
	})
	if err != nil || !created {
		t.Fatalf("task: %v %v", created, err)
	}
	tasks, err := st.ListCloudTasks(qName)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("list tasks: %v", tasks)
	}
	dt, ok, err := st.IncrementCloudTaskDispatch(taskName)
	if err != nil || !ok || dt.DispatchCount < 1 {
		t.Fatalf("dispatch: %#v", dt)
	}
	if err := st.IncrementCloudTaskResponse(taskName); err != nil {
		t.Fatal(err)
	}

	if ok, err := st.DeleteCloudTask(taskName); err != nil || !ok {
		t.Fatalf("del task: %v %v", ok, err)
	}
	if ok, err := st.DeleteCloudTasksQueue(qName); err != nil || !ok {
		t.Fatalf("del q: %v %v", ok, err)
	}
	if ok, err := st.DeleteSchedulerJob(schedName); err != nil || !ok {
		t.Fatalf("del sched: %v %v", ok, err)
	}
	if ok, err := st.DeleteCloudFunction(fnName); err != nil || !ok {
		t.Fatalf("del fn: %v %v", ok, err)
	}
	if ok, err := st.DeleteRunJob(jobName); err != nil || !ok {
		t.Fatalf("del job: %v %v", ok, err)
	}
	if ok, err := st.DeleteRunService(svcName); err != nil || !ok {
		t.Fatalf("del svc: %v %v", ok, err)
	}
}
