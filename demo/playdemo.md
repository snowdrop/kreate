## How to record the scenario execution
### Introduction  
We use `asciinema` tool to record the scenario, so the first step consist of [installing this tool](https://asciinema.org/docs/getting-started).   
  
In order to simulate the writing by hand of commands we use the `pv`, a tool for monitoring the progress of data through a pipe. So, proceed to install it:  
```  
brew install pv  
```  
### Instructions
- Launch the recording by running `asciinema rec -c ./hal/demo/play-demo-for-recording.sh` you start a new recording session.  
If you want to shrink "long time of nothing" then you can use the `-w` switch, i.e. `asciinema rec -w 2`  
Recording finishes when the scripts ends (you can skip it with Ctrl+C or upload `Enter`)  
  
Once the demo is over, asciinema will display the url where the video is available