package findings

import "testing"

func TestParseLambdaPolicy(t *testing.T) {
	doc := `{"Version":"2012-10-17","Id":"default","Statement":[
	  {"Sid":"s3","Effect":"Allow","Principal":{"Service":"s3.amazonaws.com"},"Action":"lambda:InvokeFunction",
	   "Condition":{"StringEquals":{"AWS:SourceAccount":"111122223333"},"ArnLike":{"AWS:SourceArn":"arn:aws:s3:::uploads"}}},
	  {"Sid":"url","Effect":"Allow","Principal":"*","Action":"lambda:InvokeFunctionUrl",
	   "Condition":{"StringEquals":{"lambda:FunctionUrlAuthType":"NONE"}}},
	  {"Sid":"open","Effect":"Allow","Principal":{"AWS":"*"},"Action":["lambda:InvokeFunction"]},
	  {"Sid":"xacct","Effect":"Allow","Principal":{"AWS":["arn:aws:iam::444455556666:root"]},"Action":"lambda:GetFunction"},
	  {"Sid":"deny","Effect":"Deny","Principal":"*","Action":"lambda:InvokeFunction"}
	]}`
	grants, err := ParseLambdaPolicy(doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 4 {
		t.Fatalf("want 4 grants (Deny skipped), got %d: %+v", len(grants), grants)
	}
	s3 := grants[0]
	if s3.Kind != "service" || s3.Principal != "s3.amazonaws.com" || s3.SourceArn != "arn:aws:s3:::uploads" ||
		s3.SourceAccount != "111122223333" || !s3.Invokes() || s3.Unrestricted() {
		t.Errorf("s3 grant = %+v", s3)
	}
	if url := grants[1]; url.Kind != "any" || url.URLAuthType != "NONE" || url.Unrestricted() {
		t.Errorf("a URL grant is conditioned on the auth type, not unrestricted: %+v", url)
	}
	if open := grants[2]; !open.Unrestricted() {
		t.Errorf("Principal AWS:* with no condition is unrestricted: %+v", open)
	}
	if x := grants[3]; x.Kind != "aws" || x.Invokes() {
		t.Errorf("GetFunction does not invoke: %+v", x)
	}

	// A single-object Statement is accepted too; malformed JSON is an error.
	if g, err := ParseLambdaPolicy(`{"Statement":{"Effect":"Allow","Principal":"*","Action":"lambda:*"}}`); err != nil || len(g) != 1 || !g[0].Unrestricted() {
		t.Errorf("single statement = %+v, %v", g, err)
	}
	if _, err := ParseLambdaPolicy(`{not json`); err == nil {
		t.Error("malformed policy should be an error")
	}
}
