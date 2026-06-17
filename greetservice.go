package main

type GreetService struct{}

func (g *GreetService) Greet(name string) string {
	return "Hello " + name + "!"
}

func (g *GreetService) SetAlwaysOnTop(alwaysOnTop bool) {
	//if window := application.Get().GetWindowByName("main"); window != nil {
	//	window.SetAlwaysOnTop(alwaysOnTop)
	//}
}

func (g *GreetService) Minimize() {
	//if window := application.Get().GetWindowByName("main"); window != nil {
	//	window.Minimise()
	//}
}

func (g *GreetService) Close() {
	//if window := application.Get().GetWindowByName("main"); window != nil {
	//	//window.Close()
	//}
}

func (g *GreetService) Maximize() {
	//if window := application.Get().GetWindowByName("main"); window != nil {
	//	if window.IsMaximised() {
	//		window.UnMaximise()
	//	} else {
	//		window.Maximise()
	//	}
	//}
}
