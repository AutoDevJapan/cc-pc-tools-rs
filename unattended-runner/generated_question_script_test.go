package main

import "testing"

func TestGeneratedQuestionScriptCannotHideCodeAfterJSLineTerminators(t *testing.T) {
	for _, newline := range []string{"\r", "\u2028", "\u2029"} {
		code := "// comment" + newline + `const tools={image_gen__imagegen:async()=>load("copied-image")};` + "\n" + `const result=await tools.image_gen__imagegen({prompt:"fictional UI",transparent_background:false});generatedImage(result);`
		if err := validateGeneratedScript(code, 1); err == nil {
			t.Fatal("line-comment terminator hid executable code")
		}
	}
}

func TestGeneratedQuestionScriptOnlyEmitsActualGenerationResults(t *testing.T) {
	for _, code := range []string{
		`const result=await tools.image_gen__imagegen({prompt:"fictional UI",transparent_background:false});generatedImage(result);`,
		`const prompt=load("prompt").replace("old","new").concat(" fictional");store("saved",prompt);const result=await tools.image_gen__imagegen({prompt});store("image",result);generatedImage(result);`,
		`const result=await tools.image_gen__imagegen({"transparent_background":true,"prompt":"fictional UI"});generatedImage(result);`,
	} {
		if err := validateGeneratedScript(code, 1); err != nil {
			t.Fatal("normal generation protocol rejected", err)
		}
	}
	parallel := `const promptA=load("a").concat(" fictional");const promptB=load("b");const pA=tools.image_gen__imagegen({prompt:promptA,transparent_background:false});const pB=tools.image_gen__imagegen({prompt:promptB,transparent_background:false});await yield_control();const [a,b]=await Promise.allSettled([pA,pB]);for(const [key,result] of [["a",a],["b",b]]){if(result.status==="fulfilled"){store(key,result.value);generatedImage(result.value);}else{text({key,error:String(result.reason)});}}`
	if err := validateGeneratedScript(parallel, 2); err != nil {
		t.Fatal("parallel generation protocol rejected", err)
	}
	for _, code := range []string{
		`const p=tools.image_gen__imagegen({prompt:"UI"});generatedImage(load("fake"));`,
		`const tools=load("fake-tools");const result=await tools.image_gen__imagegen({prompt:"UI"});generatedImage(result);`,
		`const generatedImage=load("fake-emitter");const result=await tools.image_gen__imagegen({prompt:"UI"});generatedImage(result);`,
		`const result=await tools.image_gen__imagegen({prompt:"UI",referenced_image_paths:load("private")});generatedImage(result);`,
		`const result=await tools.image_gen__imagegen({prompt:"UI"});const fake=load("old");generatedImage(fake);`,
		`const result=await tools.image_gen__imagegen({prompt:"UI"});result.image_url=load("old");generatedImage(result);`,
		`const result=await tools.image_gen__imagegen({prompt:"UI"});generatedImage(result);generatedImage(result);`,
		`const result=await tools.image_gen__imagegen({prompt:"UI"});generatedImage({image_url:load("old")});`,
	} {
		if err := validateGeneratedScript(code, 1); err == nil {
			t.Fatal("unrelated image or modified generation flow accepted")
		}
	}
}
